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

package db

import (
	"testing"
)

// TestRegisterPlayerCreatesAccount verifies that RegisterPlayer creates an account
// with zero permissions and the correct IPID linkage.
func TestRegisterPlayerCreatesAccount(t *testing.T) {
	teardown := setupTestDB(t)
	defer teardown()

	if err := RegisterPlayer("testplayer", []byte("secret123"), "ipid_abc"); err != nil {
		t.Fatalf("RegisterPlayer failed: %v", err)
	}

	if !UserExists("testplayer") {
		t.Fatal("account should exist after RegisterPlayer")
	}

	// Permissions must be zero — no extra powers.
	ok, perms := AuthenticateUser("testplayer", []byte("secret123"))
	if !ok {
		t.Fatal("should be able to authenticate with registered credentials")
	}
	if perms != 0 {
		t.Errorf("expected permissions=0, got %d", perms)
	}
}

// TestRegisterPlayerLinksIPID verifies that the IPID is immediately resolvable
// to the new account name after RegisterPlayer.
func TestRegisterPlayerLinksIPID(t *testing.T) {
	teardown := setupTestDB(t)
	defer teardown()

	if err := RegisterPlayer("linktest", []byte("password1"), "ipid_xyz"); err != nil {
		t.Fatalf("RegisterPlayer failed: %v", err)
	}

	username, err := GetUsernameByIPID("ipid_xyz")
	if err != nil {
		t.Fatalf("GetUsernameByIPID failed: %v", err)
	}
	if username != "linktest" {
		t.Errorf("expected username 'linktest', got %q", username)
	}
}

// TestGetUsernameByIPIDNotFound verifies that an unknown IPID returns ("", nil).
func TestGetUsernameByIPIDNotFound(t *testing.T) {
	teardown := setupTestDB(t)
	defer teardown()

	username, err := GetUsernameByIPID("unknown_ipid")
	if err != nil {
		t.Fatalf("GetUsernameByIPID returned unexpected error: %v", err)
	}
	if username != "" {
		t.Errorf("expected empty string for unknown IPID, got %q", username)
	}
}

// TestGetAllPlaytimeEntries verifies that the unpaginated leaderboard query
// returns every linked account with playtime, ordered descending by playtime,
// and excludes anonymous IPIDs and zero-playtime rows. This query backs the
// real-time /playtime top board, which merges live session time and sorts
// globally before paginating.
func TestGetAllPlaytimeEntries(t *testing.T) {
	teardown := setupTestDB(t)
	defer teardown()

	accounts := []struct {
		name string
		ipid string
		secs int64
	}{
		{"lowrank", "ipid_pt_low", 600},
		{"highrank", "ipid_pt_high", 2 * 3600},
		{"midrank", "ipid_pt_mid", 3600},
	}
	for _, a := range accounts {
		if err := RegisterPlayer(a.name, []byte("password1"), a.ipid); err != nil {
			t.Fatalf("RegisterPlayer(%s) failed: %v", a.name, err)
		}
		if err := MarkIPKnown(a.ipid); err != nil {
			t.Fatalf("MarkIPKnown(%s) failed: %v", a.name, err)
		}
		if err := AddPlaytime(a.ipid, a.secs); err != nil {
			t.Fatalf("AddPlaytime(%s) failed: %v", a.name, err)
		}
	}

	// Anonymous IPID with playtime but no USERS row: never on the leaderboard.
	if err := MarkIPKnown("ipid_pt_anon"); err != nil {
		t.Fatalf("MarkIPKnown(anon) failed: %v", err)
	}
	if err := AddPlaytime("ipid_pt_anon", 99999); err != nil {
		t.Fatalf("AddPlaytime(anon) failed: %v", err)
	}

	entries, err := GetAllPlaytimeEntries()
	if err != nil {
		t.Fatalf("GetAllPlaytimeEntries failed: %v", err)
	}
	if len(entries) != len(accounts) {
		t.Fatalf("expected %d entries, got %d", len(accounts), len(entries))
	}
	wantNames := []string{"highrank", "midrank", "lowrank"}
	wantTimes := []int64{2 * 3600, 3600, 600}
	for i := range wantNames {
		if entries[i].Username != wantNames[i] {
			t.Errorf("entry %d: expected %q, got %q", i, wantNames[i], entries[i].Username)
		}
		if entries[i].Playtime != wantTimes[i] {
			t.Errorf("entry %d (%s): expected %d seconds, got %d",
				i, wantNames[i], wantTimes[i], entries[i].Playtime)
		}
		if entries[i].Ipid == "" {
			t.Errorf("entry %d (%s): expected IPID to be populated for live merge", i, wantNames[i])
		}
	}
}

// TestLinkIPIDToUser verifies that LinkIPIDToUser updates the stored IPID.
func TestLinkIPIDToUser(t *testing.T) {
	teardown := setupTestDB(t)
	defer teardown()

	// Create a mod account via the standard path (no IPID set yet).
	if err := CreateUser("moduser", []byte("modpass"), 0xFFFF); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Simulate login linking the IPID.
	if err := LinkIPIDToUser("moduser", "ipid_mod1"); err != nil {
		t.Fatalf("LinkIPIDToUser failed: %v", err)
	}

	username, err := GetUsernameByIPID("ipid_mod1")
	if err != nil {
		t.Fatalf("GetUsernameByIPID failed: %v", err)
	}
	if username != "moduser" {
		t.Errorf("expected 'moduser', got %q", username)
	}
}

// TestLinkIPIDToUserUpdatesOnRelogin verifies that logging in from a new
// connection updates the IPID association.
func TestLinkIPIDToUserUpdatesOnRelogin(t *testing.T) {
	teardown := setupTestDB(t)
	defer teardown()

	if err := RegisterPlayer("roamer", []byte("pass12345"), "ipid_old"); err != nil {
		t.Fatalf("RegisterPlayer failed: %v", err)
	}

	// Simulate a new login from a different connection IPID.
	if err := LinkIPIDToUser("roamer", "ipid_new"); err != nil {
		t.Fatalf("LinkIPIDToUser failed: %v", err)
	}

	// Old IPID should now return nothing.
	u, _ := GetUsernameByIPID("ipid_old")
	if u != "" {
		t.Errorf("old IPID should no longer be linked, but got %q", u)
	}

	// New IPID should resolve to the account.
	u, _ = GetUsernameByIPID("ipid_new")
	if u != "roamer" {
		t.Errorf("expected 'roamer' for new IPID, got %q", u)
	}
}

// TestLinkIPIDToUserMergesPlaytime verifies that when a player re-logs from a
// new IP address, the playtime accumulated under their old IPID is transferred
// to the new IPID so the leaderboard continues to show the correct total.
func TestLinkIPIDToUserMergesPlaytime(t *testing.T) {
	teardown := setupTestDB(t)
	defer teardown()

	// Register from old IPID and simulate accumulated playtime.
	if err := RegisterPlayer("migrant", []byte("pass1234"), "migrate_old"); err != nil {
		t.Fatalf("RegisterPlayer failed: %v", err)
	}
	if err := MarkIPKnown("migrate_old"); err != nil {
		t.Fatalf("MarkIPKnown (old) failed: %v", err)
	}
	if err := AddPlaytime("migrate_old", 3600); err != nil {
		t.Fatalf("AddPlaytime failed: %v", err)
	}

	// Simulate the new connection and re-login from a different IP.
	if err := MarkIPKnown("migrate_new"); err != nil {
		t.Fatalf("MarkIPKnown (new) failed: %v", err)
	}
	if err := LinkIPIDToUser("migrant", "migrate_new"); err != nil {
		t.Fatalf("LinkIPIDToUser failed: %v", err)
	}

	// Old IPID's playtime should have been zeroed out.
	oldPT, err := GetPlaytime("migrate_old")
	if err != nil {
		t.Fatalf("GetPlaytime (old) failed: %v", err)
	}
	if oldPT != 0 {
		t.Errorf("expected old IPID playtime=0 after merge, got %d", oldPT)
	}

	// New IPID should have inherited the old playtime.
	newPT, err := GetPlaytime("migrate_new")
	if err != nil {
		t.Fatalf("GetPlaytime (new) failed: %v", err)
	}
	if newPT != 3600 {
		t.Errorf("expected new IPID playtime=3600 after merge, got %d", newPT)
	}
}

// TestLinkIPIDToUserMergesPlaytimeAdditive verifies that when both the old and
// new IPID already have playtime, the amounts are summed.
func TestLinkIPIDToUserMergesPlaytimeAdditive(t *testing.T) {
	teardown := setupTestDB(t)
	defer teardown()

	if err := RegisterPlayer("addplayer", []byte("pass1234"), "add_old"); err != nil {
		t.Fatalf("RegisterPlayer failed: %v", err)
	}
	if err := MarkIPKnown("add_old"); err != nil {
		t.Fatalf("MarkIPKnown (old) failed: %v", err)
	}
	if err := AddPlaytime("add_old", 1000); err != nil {
		t.Fatalf("AddPlaytime (old) failed: %v", err)
	}

	if err := MarkIPKnown("add_new"); err != nil {
		t.Fatalf("MarkIPKnown (new) failed: %v", err)
	}
	if err := AddPlaytime("add_new", 500); err != nil {
		t.Fatalf("AddPlaytime (new) failed: %v", err)
	}

	if err := LinkIPIDToUser("addplayer", "add_new"); err != nil {
		t.Fatalf("LinkIPIDToUser failed: %v", err)
	}

	newPT, err := GetPlaytime("add_new")
	if err != nil {
		t.Fatalf("GetPlaytime (new) failed: %v", err)
	}
	if newPT != 1500 {
		t.Errorf("expected combined playtime=1500, got %d", newPT)
	}

	oldPT, err := GetPlaytime("add_old")
	if err != nil {
		t.Fatalf("GetPlaytime (old) failed: %v", err)
	}
	if oldPT != 0 {
		t.Errorf("expected old IPID playtime=0 after merge, got %d", oldPT)
	}
}

// TestLinkIPIDToUserSameIPIDNoOp verifies that re-logging with the same IPID
// (no IP change) does not modify any KNOWN_IPS playtime.
func TestLinkIPIDToUserSameIPIDNoOp(t *testing.T) {
	teardown := setupTestDB(t)
	defer teardown()

	if err := RegisterPlayer("stable", []byte("pass1234"), "stable_ip"); err != nil {
		t.Fatalf("RegisterPlayer failed: %v", err)
	}
	if err := MarkIPKnown("stable_ip"); err != nil {
		t.Fatalf("MarkIPKnown failed: %v", err)
	}
	if err := AddPlaytime("stable_ip", 7200); err != nil {
		t.Fatalf("AddPlaytime failed: %v", err)
	}

	// Re-link with the same IPID — should be a no-op.
	if err := LinkIPIDToUser("stable", "stable_ip"); err != nil {
		t.Fatalf("LinkIPIDToUser failed: %v", err)
	}

	pt, err := GetPlaytime("stable_ip")
	if err != nil {
		t.Fatalf("GetPlaytime failed: %v", err)
	}
	if pt != 7200 {
		t.Errorf("expected playtime unchanged at 7200, got %d", pt)
	}
}

// TestRegisterPlayerDuplicateUsername verifies that registering the same
// username twice returns an error.
func TestRegisterPlayerDuplicateUsername(t *testing.T) {
	teardown := setupTestDB(t)
	defer teardown()

	if err := RegisterPlayer("taken", []byte("pass1234"), "ipid_1"); err != nil {
		t.Fatalf("first RegisterPlayer failed: %v", err)
	}
	if err := RegisterPlayer("taken", []byte("pass5678"), "ipid_2"); err == nil {
		t.Fatal("expected error for duplicate username, got nil")
	}
}

// TestGetUsernamesByIPIDsEmpty verifies that an empty input returns an empty map.
func TestGetUsernamesByIPIDsEmpty(t *testing.T) {
	teardown := setupTestDB(t)
	defer teardown()

	m, err := GetUsernamesByIPIDs([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m) != 0 {
		t.Errorf("expected empty map, got %v", m)
	}
}

// TestGetUsernamesByIPIDsSingle verifies that a single known IPID is resolved.
func TestGetUsernamesByIPIDsSingle(t *testing.T) {
	teardown := setupTestDB(t)
	defer teardown()

	if err := RegisterPlayer("batchuser1", []byte("pass1234"), "batch_ipid_1"); err != nil {
		t.Fatalf("RegisterPlayer failed: %v", err)
	}

	m, err := GetUsernamesByIPIDs([]string{"batch_ipid_1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["batch_ipid_1"] != "batchuser1" {
		t.Errorf("expected 'batchuser1', got %q", m["batch_ipid_1"])
	}
}

// TestGetUsernamesByIPIDsMultiple verifies that multiple IPIDs are resolved in one call.
func TestGetUsernamesByIPIDsMultiple(t *testing.T) {
	teardown := setupTestDB(t)
	defer teardown()

	if err := RegisterPlayer("bulka", []byte("pass1234"), "bulk_1"); err != nil {
		t.Fatalf("RegisterPlayer bulka: %v", err)
	}
	if err := RegisterPlayer("bulkb", []byte("pass5678"), "bulk_2"); err != nil {
		t.Fatalf("RegisterPlayer bulkb: %v", err)
	}

	m, err := GetUsernamesByIPIDs([]string{"bulk_1", "bulk_2", "bulk_unknown"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["bulk_1"] != "bulka" {
		t.Errorf("expected 'bulka', got %q", m["bulk_1"])
	}
	if m["bulk_2"] != "bulkb" {
		t.Errorf("expected 'bulkb', got %q", m["bulk_2"])
	}
	if _, ok := m["bulk_unknown"]; ok {
		t.Error("unknown IPID should be absent from the result map")
	}
}

// TestExistingModAccountsCompatible verifies that accounts created via
// CreateUser (the original admin path) still authenticate correctly after
// the IPID column migration.
func TestExistingModAccountsCompatible(t *testing.T) {
	teardown := setupTestDB(t)
	defer teardown()

	if err := CreateUser("admin", []byte("adminpass"), 0xFFFF); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	ok, perms := AuthenticateUser("admin", []byte("adminpass"))
	if !ok {
		t.Fatal("existing mod account should still authenticate")
	}
	if perms == 0 {
		t.Error("mod account should have non-zero permissions")
	}
}

// TestChangePermissionsToZeroKeepsAccount verifies the contract /removerole
// relies on: stripping a user's permissions to zero demotes them to a plain
// player account WITHOUT deleting the account. The username, password, and
// IPID linkage all survive; only the role (permissions) is reset.
func TestChangePermissionsToZeroKeepsAccount(t *testing.T) {
	teardown := setupTestDB(t)
	defer teardown()

	// Create a moderator account and link an IPID, as a real login would.
	if err := CreateUser("demoteme", []byte("modpass1"), 0xFFFF); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if err := LinkIPIDToUser("demoteme", "ipid_demote"); err != nil {
		t.Fatalf("LinkIPIDToUser failed: %v", err)
	}
	if !IsModUser("demoteme") {
		t.Fatal("account should report as a mod user before role removal")
	}

	// Remove the role (what /removerole does at the DB layer).
	if err := ChangePermissions("demoteme", 0); err != nil {
		t.Fatalf("ChangePermissions failed: %v", err)
	}

	// The account must still exist...
	if !UserExists("demoteme") {
		t.Error("account should still exist after role removal (must not be deleted)")
	}
	// ...but no longer be treated as a staff account...
	if IsModUser("demoteme") {
		t.Error("account should no longer report as a mod user after role removal")
	}
	// ...and still authenticate, now with zero permissions.
	ok, perms := AuthenticateUser("demoteme", []byte("modpass1"))
	if !ok {
		t.Error("demoted account should still authenticate with its original password")
	}
	if perms != 0 {
		t.Errorf("expected permissions=0 after role removal, got %d", perms)
	}
	// The IPID linkage must survive so chips/playtime stay attached to the account.
	if u, err := GetUsernameByIPID("ipid_demote"); err != nil || u != "demoteme" {
		t.Errorf("expected IPID to remain linked to 'demoteme', got %q (err=%v)", u, err)
	}
}

// TestLinkIPIDToUserMergesChips verifies that when a player re-logs from a new
// IP address, the chip balance accumulated under their old IPID is carried over
// to the new IPID so that players do not lose their earned chips.
func TestLinkIPIDToUserMergesChips(t *testing.T) {
	teardown := setupTestDB(t)
	defer teardown()

	// Register from old IPID and simulate accumulated chips.
	if err := RegisterPlayer("chipcarry", []byte("pass1234"), "chips_old"); err != nil {
		t.Fatalf("RegisterPlayer failed: %v", err)
	}
	if err := EnsureChipBalance("chips_old"); err != nil {
		t.Fatalf("EnsureChipBalance (old) failed: %v", err)
	}
	// Win 500 extra chips so the old balance is clearly above the default.
	if _, err := AddChips("chips_old", 500); err != nil {
		t.Fatalf("AddChips failed: %v", err)
	}
	// Old IPID should now have defaultChipBalance + 500.
	wantOld := int64(defaultChipBalance) + 500

	// Simulate a new connection: EnsureChipBalance seeds a fresh row for the new IPID.
	if err := EnsureChipBalance("chips_new"); err != nil {
		t.Fatalf("EnsureChipBalance (new) failed: %v", err)
	}

	// Re-login from a different IP triggers the IPID migration.
	if err := LinkIPIDToUser("chipcarry", "chips_new"); err != nil {
		t.Fatalf("LinkIPIDToUser failed: %v", err)
	}

	// New IPID must have at least the old balance (not the default 500).
	newBal, err := GetChipBalance("chips_new")
	if err != nil {
		t.Fatalf("GetChipBalance (new) failed: %v", err)
	}
	if newBal != wantOld {
		t.Errorf("expected new IPID chip balance=%d after migration, got %d", wantOld, newBal)
	}

	// Old IPID balance must be zeroed out so chips are not duplicated.
	oldBal, err := GetChipBalance("chips_old")
	if err != nil {
		t.Fatalf("GetChipBalance (old) failed: %v", err)
	}
	if oldBal != 0 {
		t.Errorf("expected old IPID chip balance=0 after migration, got %d", oldBal)
	}
}

// TestLinkIPIDToUserChipsKeepsHigher verifies that when the old IPID has more
// chips than the new IPID, the old (larger) balance is preserved on migration.
func TestLinkIPIDToUserChipsKeepsHigher(t *testing.T) {
	teardown := setupTestDB(t)
	defer teardown()

	if err := RegisterPlayer("highchip", []byte("pass1234"), "hc_old"); err != nil {
		t.Fatalf("RegisterPlayer failed: %v", err)
	}
	if err := EnsureChipBalance("hc_old"); err != nil {
		t.Fatalf("EnsureChipBalance (old) failed: %v", err)
	}
	// Old IPID earns chips: defaultChipBalance + 1000.
	if _, err := AddChips("hc_old", 1000); err != nil {
		t.Fatalf("AddChips (old) failed: %v", err)
	}

	if err := EnsureChipBalance("hc_new"); err != nil {
		t.Fatalf("EnsureChipBalance (new) failed: %v", err)
	}
	// New IPID earns chips too before login: defaultChipBalance + 200.
	if _, err := AddChips("hc_new", 200); err != nil {
		t.Fatalf("AddChips (new) failed: %v", err)
	}

	if err := LinkIPIDToUser("highchip", "hc_new"); err != nil {
		t.Fatalf("LinkIPIDToUser failed: %v", err)
	}

	// New IPID should have the old (higher) balance.
	want := int64(defaultChipBalance) + 1000
	newBal, err := GetChipBalance("hc_new")
	if err != nil {
		t.Fatalf("GetChipBalance (new) failed: %v", err)
	}
	if newBal != want {
		t.Errorf("expected new balance=%d (the higher value), got %d", want, newBal)
	}

	// Old IPID balance zeroed.
	oldBal, err := GetChipBalance("hc_old")
	if err != nil {
		t.Fatalf("GetChipBalance (old) failed: %v", err)
	}
	if oldBal != 0 {
		t.Errorf("expected old IPID chip balance=0 after migration, got %d", oldBal)
	}
}

// TestLinkIPIDToUserChipsOldWinsWhenLower verifies the fix for the bug where a
// player who had spent chips (old balance < default) would have their balance
// silently reset to the default 500 when logging in from a new IP address.
// The old IPID's earned balance must always replace the new IPID's placeholder.
func TestLinkIPIDToUserChipsOldWinsWhenLower(t *testing.T) {
	teardown := setupTestDB(t)
	defer teardown()

	if err := RegisterPlayer("lowchip", []byte("pass1234"), "lc_old"); err != nil {
		t.Fatalf("RegisterPlayer failed: %v", err)
	}
	if err := EnsureChipBalance("lc_old"); err != nil {
		t.Fatalf("EnsureChipBalance (old) failed: %v", err)
	}
	// Spend most of the default balance; old IPID now has less than the default.
	spendAmount := int64(defaultChipBalance) - 160
	if _, err := SpendChips("lc_old", spendAmount); err != nil {
		t.Fatalf("SpendChips failed: %v", err)
	}
	wantBalance := int64(160)

	// New IPID gets the full default balance from EnsureChipBalance.
	if err := EnsureChipBalance("lc_new"); err != nil {
		t.Fatalf("EnsureChipBalance (new) failed: %v", err)
	}

	if err := LinkIPIDToUser("lowchip", "lc_new"); err != nil {
		t.Fatalf("LinkIPIDToUser failed: %v", err)
	}

	// New IPID must carry the old (lower) earned balance, not the default.
	newBal, err := GetChipBalance("lc_new")
	if err != nil {
		t.Fatalf("GetChipBalance (new) failed: %v", err)
	}
	if newBal != wantBalance {
		t.Errorf("expected new IPID chip balance=%d (old earned value), got %d (was reset to default)", wantBalance, newBal)
	}

	// Old IPID balance must be zeroed out.
	oldBal, err := GetChipBalance("lc_old")
	if err != nil {
		t.Fatalf("GetChipBalance (old) failed: %v", err)
	}
	if oldBal != 0 {
		t.Errorf("expected old IPID chip balance=0 after migration, got %d", oldBal)
	}
}

// linked to an account, GetUsernameByIPID returns a non-empty string, which the
// /register command uses to block a second registration attempt.
func TestOneAccountPerIPID(t *testing.T) {
	teardown := setupTestDB(t)
	defer teardown()

	ipid := "shared_ipid"

	// First registration succeeds.
	if err := RegisterPlayer("first", []byte("pass1234"), ipid); err != nil {
		t.Fatalf("first RegisterPlayer failed: %v", err)
	}

	// GetUsernameByIPID must return the first account — this is what cmdRegister
	// checks before allowing a second registration from the same IPID.
	username, err := GetUsernameByIPID(ipid)
	if err != nil {
		t.Fatalf("GetUsernameByIPID failed: %v", err)
	}
	if username != "first" {
		t.Errorf("expected 'first', got %q", username)
	}

	// Attempting to register a second account with the same IPID should be
	// blocked by cmdRegister because GetUsernameByIPID returns a non-empty value.
	// At the DB layer the INSERT succeeds (different username), so we verify the
	// gate works at the command layer by confirming the lookup returns non-empty.
	if username == "" {
		t.Error("IPID should already be linked; cmdRegister would wrongly allow a second account")
	}
}
