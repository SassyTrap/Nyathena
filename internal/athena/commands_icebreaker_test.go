package athena

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/MangosArentLiterature/Athena/internal/area"
)

func TestIcebreakerAPIResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/today" || r.Header.Get("Accept") != "application/json" {
			t.Errorf("unexpected request: %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"question":"  What  game  changed   your mind? ","category":"general","date":"2026-11-09","totalQuestions":42}`)
	}))
	defer srv.Close()
	question, err := requestIcebreaker(context.Background(), srv.URL+"/today", srv.Client())
	if err != nil || question != "What game changed your mind?" {
		t.Fatalf("unexpected question: %q, %v", question, err)
	}
}

func TestIcebreakerRejectsInvalidResponses(t *testing.T) {
	cases := []struct {
		status int
		body   string
	}{
		{http.StatusServiceUnavailable, `{"error":"maintenance"}`},
		{http.StatusOK, `{"question":"  "}`},
		{http.StatusOK, `{"other":"not a question"}`},
		{http.StatusOK, `invalid JSON`},
		{http.StatusOK, `{"question":"` + strings.Repeat("a", 3001) + `"}`},
	}
	for i, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			fmt.Fprint(w, tc.body)
		}))
		if result, err := requestIcebreaker(context.Background(), srv.URL, srv.Client()); err == nil {
			t.Errorf("case %d accepted invalid response: %q", i, result)
		}
		srv.Close()
	}
}

func TestIcebreakerCachesMemoryAndDisk(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, `{"question":"What's your favorite courtroom game?"}`)
	}))
	defer srv.Close()
	day, path := "2026-11-09", filepath.Join(t.TempDir(), "cache.json")
	var cache icebreakerQuestionCache
	for i := 0; i < 2; i++ {
		if q := cache.get(context.Background(), day, srv.URL, path, srv.Client(), false); q != "What's your favorite courtroom game?" {
			t.Fatalf("wrong question %d: %s", i, q)
		}
	}
	var afterRestart icebreakerQuestionCache
	if q := afterRestart.get(context.Background(), day, "http://127.0.0.1:1/unavailable", path, srv.Client(), false); q != "What's your favorite courtroom game?" {
		t.Fatalf("disk cache wasn't reused: %q", q)
	}
	if hits.Load() != 1 {
		t.Fatalf("expected one fetch, got %d", hits.Load())
	}
}

func TestIcebreakerOfflineMakesNoRequestsOrWrites(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, `{"question":"Network question"}`)
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "cache.json")
	var cache icebreakerQuestionCache
	for i := 0; i < 2; i++ {
		if q := cache.get(context.Background(), "2026-11-09", srv.URL, path, srv.Client(), true); q != offlineIcebreaker("2026-11-09") {
			t.Fatalf("wrong offline question: %q", q)
		}
	}
	if hits.Load() != 0 {
		t.Fatal("offline mode made an HTTP request")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("offline mode wrote cache file: %v", err)
	}
}

func TestIcebreakerFallsBackOnAPIFailure(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "cache.json")
	var cache icebreakerQuestionCache
	for i := 0; i < 2; i++ {
		if q := cache.get(context.Background(), "2026-11-09", srv.URL, path, srv.Client(), false); q != offlineIcebreaker("2026-11-09") {
			t.Fatalf("wrong fallback question %d: %q", i, q)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("expected one failing fetch, got %d", hits.Load())
	}
}

func TestIcebreakerReadOnlyCacheIsOptional(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"question":"What's your favorite snack?"}`)
	}))
	defer srv.Close()
	var cache icebreakerQuestionCache
	// The cache path is an existing directory, so an atomic rename cannot replace it.
	if q := cache.get(context.Background(), "2026-11-09", srv.URL, t.TempDir(), srv.Client(), false); q != "What's your favorite snack?" {
		t.Fatalf("cache write failure blocked the question: %q", q)
	}
}

func TestIcebreakerRoomCooldownAndSessionLimit(t *testing.T) {
	icebreakerSessions.Lock()
	icebreakerSessions.busy = make(map[*area.Area]bool)
	icebreakerSessions.last = make(map[*area.Area]time.Time)
	icebreakerSessions.active = 0
	icebreakerSessions.Unlock()
	now := time.Now()
	rooms := []*area.Area{new(area.Area), new(area.Area), new(area.Area), new(area.Area)}
	for i := 0; i < 3; i++ {
		if ok, reason := icebreakerReserve(rooms[i], now); !ok {
			t.Fatalf("room %d: %s", i, reason)
		}
	}
	if ok, _ := icebreakerReserve(rooms[0], now); ok {
		t.Fatal("duplicate room accepted")
	}
	if ok, _ := icebreakerReserve(rooms[3], now); ok {
		t.Fatal("fourth simultaneous session accepted")
	}
	icebreakerRelease(rooms[0], true, now)
	if ok, _ := icebreakerReserve(rooms[0], now); ok {
		t.Fatal("cooldown ignored")
	}
	if ok, _ := icebreakerReserve(rooms[3], now); !ok {
		t.Fatal("session slot wasn't released")
	}
	for _, room := range rooms {
		icebreakerRelease(room, false, now)
	}
	if ok, reason := icebreakerReserve(rooms[0], now.Add(icebreakerCooldown)); !ok {
		t.Fatalf("cooldown didn't expire: %s", reason)
	}
	icebreakerRelease(rooms[0], false, now)
}

func TestIcebreakerChunksKeepText(t *testing.T) {
	source := strings.Repeat("chat about games ", 30) + "法廷 is great"
	chunks := splitIcebreaker(source, 36)
	if len(chunks) < 2 {
		t.Fatal("expected multiple chunks")
	}
	for _, chunk := range chunks {
		if utf8.RuneCountInString(chunk) > 36 {
			t.Fatalf("oversized chunk %q", chunk)
		}
	}
	if strings.Join(chunks, " ") != strings.Join(strings.Fields(source), " ") {
		t.Fatal("chunking changed the question")
	}
	longWord := strings.Repeat("λ", 260)
	parts := splitIcebreaker(longWord, 250)
	if len(parts) != 2 || strings.Join(parts, "") != longWord {
		t.Fatalf("long word lost text: %v", parts)
	}
}

func TestOfflineIcebreakerStableByDate(t *testing.T) {
	if len(offlineIcebreakers) < 10 {
		t.Fatal("not enough built-in questions")
	}
	first := offlineIcebreaker("2026-11-09")
	if first == "" || first != offlineIcebreaker("2026-11-09") {
		t.Fatal("question is not stable within the day")
	}
	if first == offlineIcebreaker("2026-11-10") {
		t.Fatal("question didn't change the next day")
	}
}
