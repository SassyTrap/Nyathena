package athena

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/MangosArentLiterature/Athena/internal/area"
)

func TestDailyQuoteChunksPreserveWordsAndLength(t *testing.T) {
	source := strings.Repeat("courtroom testimony ", 50) + "法廷 is open"
	parts := splitDailyQuote(source, 37)
	if len(parts) < 2 {
		t.Fatal("expected multiple parts")
	}
	for _, part := range parts {
		if utf8.RuneCountInString(part) > 37 {
			t.Fatalf("oversized: %q", part)
		}
	}
	if strings.Join(parts, " ") != strings.Join(strings.Fields(source), " ") {
		t.Fatalf("quote changed during splitting")
	}
	huge := strings.Repeat("λ", 261)
	parts = splitDailyQuote(huge, 250)
	if len(parts) != 2 || strings.Join(parts, "") != huge {
		t.Fatalf("long word lost characters: %#v", parts)
	}
}

func TestDailyQuoteCachesTodayInMemoryAndOnDisk(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"q":"Test # quote & percent %","a":"Test Author"}]`)
	}))
	defer server.Close()
	cachePath := filepath.Join(t.TempDir(), "cache.json")
	var cache zenDailyQuoteCache
	day := "2025-01-02"
	for i := 0; i < 2; i++ {
		item, err := cache.get(context.Background(), day, server.URL, cachePath, server.Client())
		if err != nil || item.Quote != "Test # quote & percent %" {
			t.Fatalf("fetch %d: %+v, %v", i, item, err)
		}
	}
	var afterRestart zenDailyQuoteCache
	if _, err := afterRestart.get(context.Background(), day, "http://localhost:1/invalid", cachePath, server.Client()); err != nil {
		t.Fatalf("disk cache wasn't reused: %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("expected 1 API request, got %d", hits.Load())
	}
}

func TestDailyQuoteBadAPIResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	if _, err := requestZenDailyQuote(context.Background(), srv.URL, srv.Client()); err == nil {
		t.Fatal("expected API rate-limit response to be rejected")
	}
}

func TestDailyQuoteRoomLimitAndCooldown(t *testing.T) {
	dailyQuoteSessions.Lock()
	dailyQuoteSessions.busy = make(map[*area.Area]bool)
	dailyQuoteSessions.last = make(map[*area.Area]time.Time)
	dailyQuoteSessions.active = 0
	dailyQuoteSessions.Unlock()
	now := time.Now()
	rooms := []*area.Area{new(area.Area), new(area.Area), new(area.Area), new(area.Area)}
	for i := 0; i < 3; i++ {
		if ok, reason := dailyQuoteReserve(rooms[i], now); !ok {
			t.Fatalf("room %d: %s", i, reason)
		}
	}
	if ok, _ := dailyQuoteReserve(rooms[0], now); ok {
		t.Fatal("duplicate room accepted")
	}
	if ok, _ := dailyQuoteReserve(rooms[3], now); ok {
		t.Fatal("fourth room accepted")
	}
	dailyQuoteRelease(rooms[0], true, now)
	if ok, _ := dailyQuoteReserve(rooms[0], now); ok {
		t.Fatal("room cooldown ignored")
	}
	if ok, _ := dailyQuoteReserve(rooms[3], now); !ok {
		t.Fatal("released slot wasn't reused")
	}
	for _, room := range rooms {
		dailyQuoteRelease(room, false, now)
	}
}
