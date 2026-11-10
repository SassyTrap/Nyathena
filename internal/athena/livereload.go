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
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/MangosArentLiterature/Athena/internal/logger"
	"github.com/MangosArentLiterature/Athena/internal/settings"
)

// Live, hot-reloadable server data.
//
// The character list, music list, background list, parrot list, 8-ball answers,
// CDN whitelist, automod word list and the derived lookup/packet caches used to
// be plain package globals that were written once at startup and then read
// locklessly from every connection goroutine. That is only safe while they are
// never written again. To support `/reload` (swapping them at runtime) without
// introducing a data race, each one now lives behind an atomic.Pointer:
//
//   - Readers call the get* accessors, which perform a single lock-free
//     atomic load and return the current immutable snapshot. The backing array
//     of a published snapshot is never mutated, so a reader that has already
//     loaded a slice keeps using it safely even while a reload publishes a new
//     one.
//   - A reload builds brand-new slices/strings off to the side and publishes
//     them with the set* helpers (one atomic store each). reloadMu serializes
//     concurrent reloads; readers never take it.
//
// This is the standard "rarely-written, frequently-read config" pattern and is
// clean under `go test -race`.
var (
	charactersPtr      atomic.Pointer[[]string]
	charIndexPtr       atomic.Pointer[map[string]int]
	umaCharIDsPtr      atomic.Pointer[[]int]
	musicPtr           atomic.Pointer[[]string]
	backgroundsPtr     atomic.Pointer[[]string]
	bgListStrPtr       atomic.Pointer[string]
	parrotPtr          atomic.Pointer[[]string]
	eightBallPtr       atomic.Pointer[[]string]
	cdnsPtr            atomic.Pointer[[]string]
	bannedWordsPtr     atomic.Pointer[[]string]
	censoredNamesPtr   atomic.Pointer[[]string]
	punishmentNamesPtr atomic.Pointer[[]string]
	smPacketPtr        atomic.Pointer[string]
	// areaNamesPtr is the "#"-joined area-name list embedded in the SM blob.
	// Behind a pointer like the rest because /area rename rewrites it at
	// runtime while pktAM reads it on the music/area-change path.
	areaNamesPtr atomic.Pointer[string]
	// smPacketBytes is the same blob pre-converted for the outbound queue, so
	// the join path never converts (or worse, blocking-writes) per connection.
	// Published in lockstep with smPacketPtr by setSMPacket.
	smPacketBytes atomic.Pointer[[]byte]
)

// reloadMu serializes calls to ReloadConfig so two concurrent reloads cannot
// interleave their read-validate-publish steps. The read path never takes it.
var reloadMu sync.Mutex

func loadStrSlice(p *atomic.Pointer[[]string]) []string {
	if v := p.Load(); v != nil {
		return *v
	}
	return nil
}

func storeStrSlice(p *atomic.Pointer[[]string], v []string) { p.Store(&v) }

// get* accessors — drop-in replacements for the former package globals. Each is
// a single atomic load; safe to call from any goroutine, including the hot IC
// path.

func getCharacters() []string { return loadStrSlice(&charactersPtr) }

func getCharacterIndex() map[string]int {
	if v := charIndexPtr.Load(); v != nil {
		return *v
	}
	return nil
}

func getMusicList() []string   { return loadStrSlice(&musicPtr) }
func getBackgrounds() []string { return loadStrSlice(&backgroundsPtr) }

func getBgListStr() string {
	if v := bgListStrPtr.Load(); v != nil {
		return *v
	}
	return ""
}

func getParrotList() []string      { return loadStrSlice(&parrotPtr) }
func getEightBall() []string       { return loadStrSlice(&eightBallPtr) }
func getCDNs() []string            { return loadStrSlice(&cdnsPtr) }
func getBannedWords() []string     { return loadStrSlice(&bannedWordsPtr) }
func getCensoredNames() []string   { return loadStrSlice(&censoredNamesPtr) }
func getPunishmentNames() []string { return loadStrSlice(&punishmentNamesPtr) }

func getSMPacket() string {
	if v := smPacketPtr.Load(); v != nil {
		return *v
	}
	return ""
}

// getAreaNames returns the "#"-joined list of current area names.
func getAreaNames() string {
	if v := areaNamesPtr.Load(); v != nil {
		return *v
	}
	return ""
}

func setAreaNames(s string) { areaNamesPtr.Store(&s) }

// set* helpers publish a new snapshot. setCharacters and setBackgrounds also
// rebuild their derived caches (the name→ID index and the /bglist string) so
// callers never have to keep them in sync by hand.

func setCharacters(chars []string) {
	idx := buildCharIndex(chars)
	uma := buildUmaCharIDs(chars)
	// Publish the index first, then the list: getCharacterID consults the index
	// and falls back to a linear scan of the list, so during the brief window
	// between the two stores a lookup is still correct (it just may scan the old
	// list). Both are append-only relative to each other, so no lookup breaks.
	//
	// The uma pool is published in the same order and for the same reason: its
	// entries are slots into the character list, so publishing it before the
	// list means it can only ever be a subset of what a reader can index.
	charIndexPtr.Store(&idx)
	umaCharIDsPtr.Store(&uma)
	storeStrSlice(&charactersPtr, chars)
}

func setMusicList(m []string) { storeStrSlice(&musicPtr, m) }

func setBackgrounds(bg []string) {
	s := buildBgListStr(bg)
	bgListStrPtr.Store(&s)
	storeStrSlice(&backgroundsPtr, bg)
}

func setParrotList(p []string)      { storeStrSlice(&parrotPtr, p) }
func setEightBall(e []string)       { storeStrSlice(&eightBallPtr, e) }
func setCDNs(c []string)            { storeStrSlice(&cdnsPtr, c) }
func setBannedWords(w []string)     { storeStrSlice(&bannedWordsPtr, w) }
func setCensoredNames(n []string)   { storeStrSlice(&censoredNamesPtr, n) }
func setPunishmentNames(n []string) { storeStrSlice(&punishmentNamesPtr, n) }
func setSMPacket(s string) {
	smPacketPtr.Store(&s)
	b := []byte(s)
	smPacketBytes.Store(&b)
}

// getSMPacketBytes returns the pre-built SM blob ready to enqueue. The slice is
// shared by every connection that joins between reloads and must never be
// modified -- the outbound path only ever reads it.
func getSMPacketBytes() []byte {
	if v := smPacketBytes.Load(); v != nil {
		return *v
	}
	return nil
}

// buildCharIndex builds the lowercase-name → character-ID lookup map.
func buildCharIndex(chars []string) map[string]int {
	m := make(map[string]int, len(chars))
	for i, name := range chars {
		m[strings.ToLower(name)] = i
	}
	return m
}

// buildBgListStr builds the pre-formatted background list shown by /bglist.
func buildBgListStr(bg []string) string {
	var b strings.Builder
	b.Grow(len("Available backgrounds:\n") + estimateJoinedLen(bg))
	b.WriteString("Available backgrounds:\n")
	for i, name := range bg {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(name)
	}
	return b.String()
}

// equalStrSlices reports whether two string slices are element-wise equal. Used
// by ReloadConfig to skip republishing (and to report) unchanged lists.
func equalStrSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// checkCharAppendOnly validates that newChars is an append-only extension of
// oldChars: it must contain every existing entry, unchanged and in the same
// order, optionally followed by new entries. Connected AO2 clients reference
// characters by slot index, so removing, reordering or renaming an existing
// slot while clients are connected would silently desync them — those changes
// require a restart and are rejected here with a precise message.
func checkCharAppendOnly(oldChars, newChars []string) error {
	if len(newChars) < len(oldChars) {
		return fmt.Errorf("characters.txt: list shrank from %d to %d entries — character reload is append-only while clients are connected (slots can't be removed); restart the server to remove characters",
			len(oldChars), len(newChars))
	}
	for i := range oldChars {
		if oldChars[i] != newChars[i] {
			return fmt.Errorf("characters.txt: slot %d changed from %q to %q — character reload is append-only; add new characters at the END of the file (restart the server to reorder, rename or insert)",
				i, oldChars[i], newChars[i])
		}
	}
	return nil
}

// ReloadConfig re-reads the hot-reloadable config/data files from disk and
// atomically swaps in the new values. It is safe to call at runtime from the
// stdin CLI, the in-game /reload command or a signal handler.
//
// characters.txt is reloaded append-only (see checkCharAppendOnly); when new
// characters are appended, every area's character-slot table is grown first so
// the new slots can be selected without an out-of-bounds panic, and only then
// is the longer list published.
//
// A change to any file is non-destructive to the others: each file is validated
// and applied independently, and a parse error in one (e.g. a malformed
// characters.txt) aborts the whole reload before anything is published so the
// running server is never left half-updated. Returns a human-readable summary
// of what changed.
func ReloadConfig() (string, error) {
	reloadMu.Lock()
	defer reloadMu.Unlock()

	// --- Phase 1: load and validate everything from disk. Nothing is published
	// until every file parses, so a bad file leaves the live config untouched.
	newChars, err := settings.LoadFile("/characters.txt")
	if err != nil {
		return "", fmt.Errorf("characters.txt: %w", err)
	}
	if len(newChars) == 0 {
		return "", fmt.Errorf("characters.txt: empty character list")
	}
	oldChars := getCharacters()
	if err := checkCharAppendOnly(oldChars, newChars); err != nil {
		return "", err
	}

	newMusic, err := settings.LoadMusic()
	if err != nil {
		return "", fmt.Errorf("music.txt: %w", err)
	}

	newBg, err := settings.LoadFile("/backgrounds.txt")
	if err != nil {
		return "", fmt.Errorf("backgrounds.txt: %w", err)
	}
	if len(newBg) == 0 {
		return "", fmt.Errorf("backgrounds.txt: empty background list")
	}

	newParrot, err := settings.LoadFile("/parrot.txt")
	if err != nil {
		return "", fmt.Errorf("parrot.txt: %w", err)
	}
	if len(newParrot) == 0 {
		return "", fmt.Errorf("parrot.txt: empty parrot list")
	}

	newCDNs := settings.LoadCDNs()
	newBotContent, err := loadBotContent()
	if err != nil { return "", err }
	if candidate, err := settings.GetConfig(); err == nil {
		if _, err := parseBotOptions(candidate); err != nil { return "", err }
	}

	// 8ball.txt and the automod wordlist are optional; load failures leave the
	// current value in place rather than aborting the reload.
	var newEight []string
	haveEight := false
	if loaded, eerr := settings.LoadFile("/8ball.txt"); eerr == nil {
		newEight = loaded
		haveEight = true
	}

	// The banned-word list is reloaded regardless of automod_enabled — like
	// censored_names.txt below, other features (e.g. the giveaway item
	// filter) match against it independently of automod's own IC/OOC
	// enforcement toggle.
	var newBanned []string
	haveBanned := false
	if config != nil {
		path := filepath.Join(settings.ConfigPath, config.AutoModWordlist)
		if loaded, werr := loadWordListFile(path); werr == nil {
			newBanned = loaded
			haveBanned = true
		} else {
			logger.LogWarningf("reload: failed to reload automod wordlist %q: %v", path, werr)
		}
	}

	// censored_names.txt is independent of automod_enabled and optional; a
	// missing file leaves the current (possibly empty) list in place.
	var newCensored []string
	haveCensored := false
	censoredPath := filepath.Join(settings.ConfigPath, censoredNamesFile)
	if loaded, cerr := loadWordListFile(censoredPath); cerr == nil {
		newCensored = loaded
		haveCensored = true
	}

	// punishment_names.txt (showname punisher) is likewise optional and
	// independent of automod_enabled; a missing file leaves the current
	// (possibly empty) list in place.
	var newPunishNames []string
	havePunishNames := false
	punishNamesPath := filepath.Join(settings.ConfigPath, punishmentNamesFile)
	if loaded, perr := loadWordListFile(punishNamesPath); perr == nil {
		newPunishNames = loaded
		havePunishNames = true
	}

	// --- Phase 2: publish. These are atomic stores; readers see old-or-new, never
	// a torn value.
	var changes []string

	if len(newChars) != len(oldChars) {
		// Grow every area's slot table BEFORE publishing the longer list so a
		// client that picks a brand-new character index can never index past the
		// area's taken[] array. Areas only grow; existing slot state is kept.
		for _, a := range areas {
			a.GrowTaken(len(newChars))
		}
		setCharacters(newChars)
		changes = append(changes, fmt.Sprintf("characters.txt (+%d)", len(newChars)-len(oldChars)))
	}

	if !equalStrSlices(getMusicList(), newMusic) {
		setMusicList(newMusic)
		// The SM packet (sent to every client on join) embeds the music list, so
		// rebuild it from the new list and the unchanged area names.
		setSMPacket(buildSMPacket(getAreaNames(), newMusic))
		changes = append(changes, "music.txt")
	}

	if !equalStrSlices(getBackgrounds(), newBg) {
		setBackgrounds(newBg)
		changes = append(changes, "backgrounds.txt")
	}

	if !equalStrSlices(getParrotList(), newParrot) {
		setParrotList(newParrot)
		changes = append(changes, "parrot.txt")
	}

	if !equalStrSlices(getCDNs(), newCDNs) {
		setCDNs(newCDNs)
		changes = append(changes, "cdns.txt")
	}

	if haveEight && !equalStrSlices(getEightBall(), newEight) {
		setEightBall(newEight)
		changes = append(changes, "8ball.txt")
	}
	if botChanges := botContentDifferences(newBotContent); len(botChanges) > 0 {
		botContentPtr.Store(newBotContent)
		changes = append(changes, botChanges...)
	}

	if haveBanned && !equalStrSlices(getBannedWords(), newBanned) {
		setBannedWords(newBanned)
		changes = append(changes, "banned_words.txt")
	}

	if haveCensored && !equalStrSlices(getCensoredNames(), newCensored) {
		setCensoredNames(newCensored)
		changes = append(changes, "censored_names.txt")
	}

	if havePunishNames && !equalStrSlices(getPunishmentNames(), newPunishNames) {
		setPunishmentNames(newPunishNames)
		changes = append(changes, "punishment_names.txt")
	}

	// captcha_questions.txt (operator-written join-captcha questions) is
	// optional and independent of every other list; a parse error fails the
	// reload loudly rather than silently emptying the set.
	beforeCustom := len(getCustomChallenges())
	if cerr := reloadCustomChallenges(); cerr != nil {
		logger.LogWarningf("reload: %v", cerr)
	} else if len(getCustomChallenges()) != beforeCustom {
		changes = append(changes, "captcha_questions.txt")
	}

	// config.toml hot fields (motd / description).
	if n, cerr := ReloadHotConfig(); cerr != nil {
		logger.LogWarningf("reload: config.toml hot fields not reloaded: %v", cerr)
	} else if n > 0 {
		changes = append(changes, fmt.Sprintf("config.toml (%d field(s))", n))
	}

	if len(changes) == 0 {
		return "no changes detected", nil
	}
	return strings.Join(changes, ", "), nil
}
