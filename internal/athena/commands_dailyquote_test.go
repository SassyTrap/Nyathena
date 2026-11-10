package athena

import (
	"testing"
	"time"

	"github.com/MangosArentLiterature/Athena/internal/area"
)

func TestDailyQuoteLimiter(t *testing.T) {
	limiter := newBotLimiter()
	rooms := []*area.Area{{}, {}, {}, {}}
	for i := 0; i < 3; i++ {
		if ok, msg := limiter.reserve(rooms[i]); !ok {
			t.Fatalf("reserve %d: %s", i, msg)
		}
	}
	if ok, _ := limiter.reserve(rooms[0]); ok {
		t.Fatal("same room reused while busy")
	}
	if ok, _ := limiter.reserve(rooms[3]); ok {
		t.Fatal("fourth session accepted")
	}
	limiter.release(rooms[0], true)
	if ok, _ := limiter.reserve(rooms[0]); ok {
		t.Fatal("cooldown ignored")
	}
	limiter.release(rooms[1], false)
	if ok, reason := limiter.reserve(rooms[3]); !ok {
		t.Fatalf("free slot rejected: %s", reason)
	}
	limiter.mu.Lock()
	limiter.last[rooms[0]] = time.Now().Add(-botCooldown - time.Second)
	limiter.mu.Unlock()
	if ok, reason := limiter.reserve(rooms[0]); !ok {
		t.Fatalf("expired cooldown blocked: %s", reason)
	}
}
