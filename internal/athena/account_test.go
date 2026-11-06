/* Athena - A server for Attorney Online 2 written in Go
Copyright (C) 2022 MangosArentLiterature <mango@transmenace.dev>

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published
by the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>. */

package athena

import (
	"fmt"
	"strings"
	"testing"

	"github.com/MangosArentLiterature/Athena/internal/db"
)

// TestFormatPlaytime verifies that formatPlaytime converts seconds to the
// expected human-readable string for all relevant cases.
func TestFormatPlaytime(t *testing.T) {
	cases := []struct {
		secs int64
		want string
	}{
		{-1, "less than a minute"},
		{0, "less than a minute"},
		{30, "0m"},      // < 60s: minutes = 0
		{59, "0m"},      // one second short of a minute
		{60, "1m"},      // exactly one minute
		{90, "1m"},      // 1m 30s → 1m (seconds truncated)
		{3600, "1h 0m"}, // exactly one hour
		{3661, "1h 1m"}, // 1h 1m 1s
		{7322, "2h 2m"}, // 2h 2m 2s
	}
	for _, tc := range cases {
		got := formatPlaytime(tc.secs)
		if got != tc.want {
			t.Errorf("formatPlaytime(%d) = %q, want %q", tc.secs, got, tc.want)
		}
	}
}

// TestMergePlaytimeEntriesMergesLiveTimeGlobally verifies that live session
// time is added to every entry and that the WHOLE board is re-sorted. The bug
// this pins: merging live time into only the requested page left a player
// whose session pushed them up several ranks displayed below players with
// fewer hours (e.g. 4h listed after 2h).
func TestMergePlaytimeEntriesMergesLiveTimeGlobally(t *testing.T) {
	entries := []db.PlaytimeEntry{
		{Ipid: "ipid_alpha", Username: "alpha", Playtime: 3 * 3600},
		{Ipid: "ipid_bravo", Username: "bravo", Playtime: 2*3600 + 30*60},
		{Ipid: "ipid_charlie", Username: "charlie", Playtime: 2 * 3600},
	}
	// charlie has been connected for 2h; their live total (4h) must overtake
	// alpha's 3h even though their DB row was last.
	merged := mergePlaytimeEntries(entries, map[string]int64{"ipid_charlie": 2 * 3600})

	wantNames := []string{"charlie", "alpha", "bravo"}
	wantTimes := []int64{4 * 3600, 3 * 3600, 2*3600 + 30*60}
	if len(merged) != len(wantNames) {
		t.Fatalf("expected %d entries, got %d", len(wantNames), len(merged))
	}
	for i := range wantNames {
		if merged[i].Username != wantNames[i] {
			t.Errorf("entry %d: expected %q, got %q", i, wantNames[i], merged[i].Username)
		}
		if merged[i].Playtime != wantTimes[i] {
			t.Errorf("entry %d (%s): expected %d seconds, got %d",
				i, wantNames[i], wantTimes[i], merged[i].Playtime)
		}
	}

	// The caller's slice must not be modified — the DB result is still needed
	// for the total and the viewer's own position.
	if entries[2].Playtime != 2*3600 {
		t.Errorf("mergePlaytimeEntries mutated its input: charlie playtime is %d", entries[2].Playtime)
	}
}

// TestMergePlaytimeEntriesTieBreak verifies equal playtimes sort by username so
// ranks stay deterministic between pages and repeated calls.
func TestMergePlaytimeEntriesTieBreak(t *testing.T) {
	entries := []db.PlaytimeEntry{
		{Ipid: "ipid_z", Username: "zoe", Playtime: 3600},
		{Ipid: "ipid_a", Username: "alice", Playtime: 3600},
		{Ipid: "ipid_m", Username: "mallory", Playtime: 7200},
	}
	merged := mergePlaytimeEntries(entries, nil)
	want := []string{"mallory", "alice", "zoe"}
	for i := range want {
		if merged[i].Username != want[i] {
			t.Errorf("entry %d: expected %q, got %q", i, want[i], merged[i].Username)
		}
	}
}

// TestPlaytimePosition verifies the viewer's own rank lookup. A logged-in
// account name takes precedence over the connection's IPID, and the IPID link
// still works as a fallback for logged-out connections.
func TestPlaytimePosition(t *testing.T) {
	entries := []db.PlaytimeEntry{
		{Ipid: "ipid_first", Username: "first", Playtime: 7200},
		{Ipid: "ipid_second", Username: "second", Playtime: 3600},
	}
	if got := playtimePosition(entries, "ipid_second", ""); got != 2 {
		t.Errorf("expected position 2 for ipid_second, got %d", got)
	}
	// Logged in as "second" from a connection whose IPID is not the linked one
	// (e.g. the account link moved): the account name must still resolve.
	if got := playtimePosition(entries, "ipid_elsewhere", "second"); got != 2 {
		t.Errorf("expected position 2 for logged-in account 'second', got %d", got)
	}
	// Logged in as an account with no leaderboard entry: fall back to the IPID.
	if got := playtimePosition(entries, "ipid_first", "unlisted"); got != 1 {
		t.Errorf("expected IPID fallback position 1, got %d", got)
	}
	if got := playtimePosition(entries, "ipid_guest", ""); got != 0 {
		t.Errorf("expected position 0 for an unlisted IPID, got %d", got)
	}
	if got := playtimePosition(nil, "ipid_first", ""); got != 0 {
		t.Errorf("expected position 0 for an empty board, got %d", got)
	}
}

// TestSumPlaytimeSeconds verifies the server-wide total sums every entry.
func TestSumPlaytimeSeconds(t *testing.T) {
	entries := []db.PlaytimeEntry{
		{Playtime: 7200},
		{Playtime: 3600},
		{Playtime: 60},
	}
	if got := sumPlaytimeSeconds(entries); got != 10860 {
		t.Errorf("expected total 10860, got %d", got)
	}
	if got := sumPlaytimeSeconds(nil); got != 0 {
		t.Errorf("expected total 0 for an empty board, got %d", got)
	}
}

// TestFormatPlaytimeLong verifies the day-aware formatter used for the
// server-wide total.
func TestFormatPlaytimeLong(t *testing.T) {
	cases := []struct {
		secs int64
		want string
	}{
		{0, "less than a minute"},
		{59, "0m"},
		{3600, "1h 0m"},
		{86399, "23h 59m"},
		{86400, "1d 0h 0m"},
		{90061, "1d 1h 1m"},
		{10 * 86400, "10d 0h 0m"},
	}
	for _, tc := range cases {
		if got := formatPlaytimeLong(tc.secs); got != tc.want {
			t.Errorf("formatPlaytimeLong(%d) = %q, want %q", tc.secs, got, tc.want)
		}
	}
}

// TestPlaytimeTotalPages verifies the page-count math used by /playtime top,
// including the boundary that rejects an oversized page argument without
// multiplying it into an overflow.
func TestPlaytimeTotalPages(t *testing.T) {
	cases := []struct {
		total int
		want  int
	}{
		{-1, 1}, // defensive: an empty board still reports 1/1
		{0, 1},
		{1, 1},
		{25, 1},
		{26, 2},
		{252, 11},
	}
	for _, tc := range cases {
		if got := playtimeTotalPages(tc.total); got != tc.want {
			t.Errorf("playtimeTotalPages(%d) = %d, want %d", tc.total, got, tc.want)
		}
	}
	// A very large page number must simply compare greater than the page count;
	// the command uses this comparison instead of (page-1)*playtimePageSize so
	// it can never overflow into a negative slice offset.
	if !(int(^uint(0)>>1) > playtimeTotalPages(252)) {
		t.Error("max int page argument must compare above the page count")
	}
}

// TestRenderPlaytimePage verifies the board layout: the server-wide total above
// the page, rank numbering continuing across pages, the next-page footer, and
// the viewer's own standing below a --- separator so it is never counted as
// part of the paginated list.
func TestRenderPlaytimePage(t *testing.T) {
	// 27 entries → two pages; the second page holds ranks 26–27.
	board := make([]db.PlaytimeEntry, 0, 27)
	for i := 0; i < 27; i++ {
		board = append(board, db.PlaytimeEntry{
			Ipid:     fmt.Sprintf("ipid_%02d", i),
			Username: fmt.Sprintf("player%02d", i),
			Playtime: int64(27-i) * 3600,
		})
	}

	// The viewer is last on the board but viewing page 1: their position line
	// must still be shown after the separator.
	got := renderPlaytimePage(1, board, "ipid_26", "")
	if !strings.HasPrefix(got, "\n⏱ Playtime Leaderboard — Page 1/2 (27 total)\n") {
		t.Errorf("unexpected header: %q", got)
	}
	// 27+26+…+1 hours = 378h = 15d 18h 0m.
	if !strings.Contains(got, "🕒 Server total playtime: 15d 18h 0m\n") {
		t.Errorf("missing/incorrect server total line: %q", got)
	}
	if !strings.Contains(got, "  1. player00") {
		t.Errorf("missing rank 1 on page 1: %q", got)
	}
	if !strings.Contains(got, "\nUse /playtime top 2 for the next page.\n") {
		t.Errorf("missing next-page footer: %q", got)
	}
	if !strings.Contains(got, "---\n📌 Your position: #27 of 27 — player26 (1h 0m)\n") {
		t.Errorf("missing/incorrect position line: %q", got)
	}

	// Page 2 must continue the rank numbering and still show the viewer line.
	got = renderPlaytimePage(2, board, "ipid_00", "")
	if !strings.Contains(got, "⏱ Playtime Leaderboard — Page 2/2 (27 total)") {
		t.Errorf("unexpected page-2 header: %q", got)
	}
	if !strings.Contains(got, " 26. player25") {
		t.Errorf("page 2 must start at rank 26: %q", got)
	}
	if strings.Contains(got, "Use /playtime top 3") {
		t.Errorf("last page must not advertise a next page: %q", got)
	}
	if !strings.Contains(got, "📌 Your position: #1 of 27 — player00 (27h 0m)") {
		t.Errorf("missing/incorrect page-2 position line: %q", got)
	}

	// A guest has no board entry: the separator is still shown, followed by the
	// explicit not-on-the-leaderboard line.
	guest := renderPlaytimePage(1, board, "ipid_guest", "")
	if !strings.Contains(guest, "---\n📌 You are not on the leaderboard yet.") {
		t.Errorf("guest output missing not-on-leaderboard line: %q", guest)
	}

	// A logged-in viewer is matched by account name even when the connection's
	// IPID differs from the account's linked IPID.
	loggedIn := renderPlaytimePage(1, board, "ipid_other", "player26")
	if !strings.Contains(loggedIn, "---\n📌 Your position: #27 of 27 — player26 (1h 0m)\n") {
		t.Errorf("logged-in viewer position line missing/incorrect: %q", loggedIn)
	}
}

// TestGenerateCaptcha verifies that generateCaptcha returns a non-empty string
// and that two successive calls produce different tokens.
func TestGenerateCaptcha(t *testing.T) {
	tok1, err := generateCaptcha()
	if err != nil {
		t.Fatalf("generateCaptcha returned unexpected error: %v", err)
	}
	if tok1 == "" {
		t.Fatal("generateCaptcha returned an empty string")
	}
	// 16 hex characters = 8 bytes.
	if len(tok1) != 16 {
		t.Errorf("expected token length 16, got %d", len(tok1))
	}

	tok2, err := generateCaptcha()
	if err != nil {
		t.Fatalf("second generateCaptcha returned unexpected error: %v", err)
	}
	if tok1 == tok2 {
		t.Error("two successive captcha tokens should not be identical")
	}
}

// TestClientPendingReg verifies that SetPendingReg stores values and PendingReg
// retrieves them, and that clearing works correctly.
func TestClientPendingReg(t *testing.T) {
	c := &Client{}

	// Initially all fields should be zero-valued.
	u, tok, hp := c.PendingReg()
	if u != "" || tok != "" || hp != nil {
		t.Errorf("expected empty pending reg, got (user=%q, tok=%q, pass=%v)", u, tok, hp)
	}

	// Set pending registration data.
	fakeHash := []byte("$2a$12$fakehashvalue")
	c.SetPendingReg("alice", "abc123def456abcd", fakeHash)
	u, tok, hp = c.PendingReg()
	if u != "alice" {
		t.Errorf("expected username 'alice', got %q", u)
	}
	if tok != "abc123def456abcd" {
		t.Errorf("expected token 'abc123def456abcd', got %q", tok)
	}
	if string(hp) != string(fakeHash) {
		t.Errorf("expected hash %q, got %q", fakeHash, hp)
	}

	// Clear pending registration.
	c.SetPendingReg("", "", nil)
	u, tok, hp = c.PendingReg()
	if u != "" || tok != "" || hp != nil {
		t.Errorf("expected cleared pending reg, got (user=%q, tok=%q, pass=%v)", u, tok, hp)
	}
}
