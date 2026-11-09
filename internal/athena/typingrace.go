package athena

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/MangosArentLiterature/Athena/internal/db"
	"github.com/MangosArentLiterature/Athena/internal/logger"
)

const (
	typingRaceOptInDuration = 30 * time.Second // window to join before phrase is posted
	typingRaceTimeout       = 90 * time.Second // max time to complete phrase after it's posted
	typingRaceCooldown      = 3 * time.Minute  // cooldown between races
	typingRaceReward        = 50               // chips awarded to the winner
)

// typingRaceState is the mutex-protected global state for the typing race minigame.
type typingRaceState struct {
	mu           sync.Mutex
	optInActive  bool             // true during the 30-second join window
	raceActive   bool             // true while the phrase is posted and awaiting answer
	participants map[int]struct{} // UIDs who joined during opt-in
	phrase       string           // the phrase players must type (lowercased for comparison)
	phraseRaw    string           // the phrase as announced (original case)
	postedAt     time.Time        // when the phrase was posted
	lastRaceEnd  time.Time        // used to enforce cooldown
}

// typingRaceActiveFast is an atomic mirror of typingRace.raceActive.
// typingRaceOnIC reads it without acquiring the mutex so the common case
// (no race active) is a single atomic load instead of a mutex acquisition.
var typingRaceActiveFast atomic.Bool

var typingRace = typingRaceState{
	participants: make(map[int]struct{}),
}

// typingRacePhrases is the built-in phrase pool used when no custom phrases are configured.
// Each phrase should be at least 4 words so that WPS is meaningful.
var typingRacePhrases = []string{
	"the quick brown fox jumps over the lazy dog",
	"pack my box with five dozen liquor jugs",
	"how vexingly quick daft zebras jump",
	"the five boxing wizards jump quickly",
	"sphinx of black quartz judge my vow",
	"two driven jocks help fax my big quiz",
	"five quacking zephyrs jolt my wax bed",
	"the job requires extra pluck and zeal from every young wage earner",
	"a wizard's job is to vex chumps quickly in fog",
	"watch jeopardy alex trebek films",
	"blowzy night frumps vex'd jack q",
	"mr jock tv quiz phd bags few lynx",
	"objectively the quick fox jumps over lazy dogs near the river bank",
	"attorney online is a courtroom roleplay game",
	"the defendant presents new evidence at the last moment",
	"objection your honour the witness is lying",
	"hold it that testimony doesn't add up at all",
	"take that here is the evidence you forgot about",
	"the truth will come to light in this courtroom today",
	"I have evidence that proves my client is innocent",
	"the prosecution rests but justice never sleeps",
	"a contradiction in your testimony has been found",
	"never give up and never surrender the truth",
	"every great story begins with a single brave step",
	"the stars above remind us how small we truly are",
	"in the end only the facts of the case remain",
	"the verdict is in and justice has been served today",
	"logic and reason are the tools of the great detective",
	"cross examination reveals what direct testimony conceals",
	"the alibi does not hold up under scrutiny at all",
}

// cmdTypingRace is the command entry point for /typingrace.
func cmdTypingRace(client *Client, args []string, usage string) {
	if len(args) > 0 && strings.ToLower(args[0]) == "join" {
		typingRaceJoin(client)
		return
	}
	typingRaceStart(client)
}

// typingRaceStart opens the opt-in window for a new race.
func typingRaceStart(client *Client) {
	typingRace.mu.Lock()
	if typingRace.optInActive || typingRace.raceActive {
		typingRace.mu.Unlock()
		client.SendServerMessage("A typing race is already in progress.")
		return
	}
	if !typingRace.lastRaceEnd.IsZero() && time.Since(typingRace.lastRaceEnd) < typingRaceCooldown {
		remaining := typingRaceCooldown - time.Since(typingRace.lastRaceEnd)
		typingRace.mu.Unlock()
		client.SendServerMessage(fmt.Sprintf("Typing race on cooldown. Try again in %.0f seconds.", remaining.Seconds()))
		return
	}
	typingRace.optInActive = true
	typingRace.participants = make(map[int]struct{})
	typingRace.mu.Unlock()

	var announce string
	if config != nil && config.CasinoOn() {
		announce = fmt.Sprintf(
			"⌨️ TYPING RACE! Type /typingrace join in the next %.0f seconds to enter. First to type the phrase wins %d chips!",
			typingRaceOptInDuration.Seconds(), typingRaceReward,
		)
	} else {
		announce = fmt.Sprintf(
			"⌨️ TYPING RACE! Type /typingrace join in the next %.0f seconds to enter. First to type the phrase wins bragging rights!",
			typingRaceOptInDuration.Seconds(),
		)
	}
	sendGlobalServerMessage(announce)
	go typingRaceOptInTimer()
}

// typingRaceJoin adds the client to the participant list during the opt-in window.
func typingRaceJoin(client *Client) {
	typingRace.mu.Lock()
	if !typingRace.optInActive {
		typingRace.mu.Unlock()
		client.SendServerMessage("There is no typing race open to join right now.")
		return
	}
	uid := client.Uid()
	if _, already := typingRace.participants[uid]; already {
		typingRace.mu.Unlock()
		client.SendServerMessage("You have already joined the typing race.")
		return
	}
	typingRace.participants[uid] = struct{}{}
	count := len(typingRace.participants)
	typingRace.mu.Unlock()
	client.SendServerMessage(fmt.Sprintf("⌨️ Joined! (%d participant(s) so far)", count))
}

// typingRaceOptInTimer waits for the opt-in window, then begins the race.
func typingRaceOptInTimer() {
	time.Sleep(typingRaceOptInDuration)

	typingRace.mu.Lock()
	if !typingRace.optInActive {
		typingRace.mu.Unlock()
		return
	}
	typingRace.optInActive = false
	uids := make([]int, 0, len(typingRace.participants))
	for uid := range typingRace.participants {
		uids = append(uids, uid)
	}
	typingRace.mu.Unlock()

	// Filter to still-connected participants.
	n := 0
	for _, uid := range uids {
		if _, err := getClientByUid(uid); err == nil {
			uids[n] = uid
			n++
		}
	}
	uids = uids[:n]

	if len(uids) == 0 {
		typingRace.mu.Lock()
		typingRace.lastRaceEnd = time.Now()
		typingRace.mu.Unlock()
		sendGlobalServerMessage("⌨️ Typing race cancelled — no participants joined.")
		return
	}

	// Pick a phrase.
	phrases := typingRacePhrases
	if config != nil && len(config.TypingRacePhrases) > 0 {
		phrases = config.TypingRacePhrases
	}
	phraseRaw := phrases[rand.Intn(len(phrases))]
	phraseKey := normaliseTypingPhrase(phraseRaw)

	typingRace.mu.Lock()
	typingRace.raceActive = true
	typingRaceActiveFast.Store(true)
	typingRace.phrase = phraseKey
	typingRace.phraseRaw = phraseRaw
	typingRace.postedAt = time.Now()
	typingRace.mu.Unlock()

	sendGlobalServerMessage(fmt.Sprintf(
		"⌨️ RACE BEGINS! %d participant(s). Type this phrase in IC as fast as you can:\n\"%s\"\n(%.0f seconds to complete)",
		len(uids), phraseRaw, typingRaceTimeout.Seconds(),
	))

	// Auto-expire if nobody completes it.
	time.AfterFunc(typingRaceTimeout, func() {
		typingRace.mu.Lock()
		if !typingRace.raceActive || typingRace.phrase != phraseKey {
			typingRace.mu.Unlock()
			return
		}
		typingRace.raceActive = false
		typingRaceActiveFast.Store(false)
		typingRace.lastRaceEnd = time.Now()
		typingRace.mu.Unlock()
		sendGlobalServerMessage(fmt.Sprintf("⌨️ Time's up! Nobody typed the phrase in time. The answer was: \"%s\"", phraseRaw))
	})
}

// typingRaceOnIC is called from the IC packet handler for every in-character message.
// It checks whether the message matches the active race phrase.
func typingRaceOnIC(client *Client, msgText string) {
	// Atomic fast path: skip the mutex entirely when no race is active.
	// This is the common case — most IC messages arrive outside a race.
	if !typingRaceActiveFast.Load() {
		return
	}

	// Normalise before acquiring the lock so we hold the lock for the
	// minimum time necessary.
	guess := normaliseTypingPhrase(msgText)

	typingRace.mu.Lock()
	if !typingRace.raceActive {
		typingRace.mu.Unlock()
		return
	}

	uid := client.Uid()
	if _, ok := typingRace.participants[uid]; !ok {
		typingRace.mu.Unlock()
		return
	}

	if guess != typingRace.phrase {
		typingRace.mu.Unlock()
		return
	}

	// Winner!
	elapsed := time.Since(typingRace.postedAt)
	phraseRaw := typingRace.phraseRaw
	typingRace.raceActive = false
	typingRaceActiveFast.Store(false)
	typingRace.lastRaceEnd = time.Now()
	typingRace.mu.Unlock()

	wordCount := len(strings.ReplaceAll(phraseRaw, " ", "")) / 5
	minutes := elapsed.Minutes()
	var wpm float64
	if minutes > 0 {
		wpm = float64(wordCount) / minutes
	}
	wps := float64(wordCount) / elapsed.Seconds()

	// Chip rewards are only awarded when the casino/chip economy is enabled.
	// When gambling is disabled, the race still runs but nothing is paid out.
	if config != nil && config.CasinoOn() {
		newBal, chipErr := db.AddChips(client.Ipid(), typingRaceReward)
		if chipErr != nil {
			logger.LogErrorf("typingrace: AddChips failed: %v", chipErr)
		}
		sendGlobalServerMessage(fmt.Sprintf(
			"⌨️ 🏆 %v won the typing race in %.2fs (%.1f WPS / %.0f WPM)! +%d chips (balance: %d)",
			client.OOCName(), elapsed.Seconds(), wps, wpm, typingRaceReward, newBal,
		))
	} else {
		sendGlobalServerMessage(fmt.Sprintf(
			"⌨️ 🏆 %v won the typing race in %.2fs (%.1f WPS / %.0f WPM)!",
			client.OOCName(), elapsed.Seconds(), wps, wpm,
		))
	}
}

// normaliseTypingPhrase lowercases, collapses whitespace, and strips punctuation
// at word boundaries so that minor formatting differences don't disqualify a
// valid answer.
//
// Optimisation notes:
//   - Characters are lowercased inline with unicode.ToLower, avoiding the
//     intermediate string allocation that strings.ToLower would produce.
//   - A "pending space" flag defers space emission until the next valid
//     character is seen, so neither TrimSpace nor a second pass is needed.
func normaliseTypingPhrase(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	pendingSpace := false
	for _, r := range s {
		r = unicode.ToLower(r)
		if unicode.IsSpace(r) {
			if b.Len() > 0 {
				pendingSpace = true
			}
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '\'' {
			if pendingSpace {
				b.WriteByte(' ')
				pendingSpace = false
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}
