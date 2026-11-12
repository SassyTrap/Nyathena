// SPDX-License-Identifier: AGPL-3.0-or-later
package athena

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/MangosArentLiterature/Athena/internal/area"
)

const guessWhoDuration = 60 * time.Second

type guessWhoRound struct {
	character guessWhoCharacter
	timeout   *time.Timer
}

var guessWhoState = struct {
	sync.Mutex
	active map[*area.Area]*guessWhoRound
	last   map[*area.Area]time.Time
}{active: make(map[*area.Area]*guessWhoRound), last: make(map[*area.Area]time.Time)}

func cmdGuessWho(client *Client, _ []string, _ string) {
	opts := currentBotOptions()
	if !opts.GuessWho {
		client.SendServerMessage("Guess Who is not enabled on this server.")
		return
	}
	room := client.Area()
	if room == nil {
		client.SendServerMessage("Please join a room first.")
		return
	}
	characters := currentBotContent().Characters
	if len(characters) == 0 {
		client.SendServerMessage("No characters are configured in guesswho.txt.")
		return
	}
	guessWhoState.Lock()
	if guessWhoState.active[room] != nil {
		guessWhoState.Unlock()
		client.SendServerMessage("A Guess Who round is already active in this room.")
		return
	}
	if t := guessWhoState.last[room]; !t.IsZero() && time.Since(t) < botCooldown {
		remaining := botCooldown - time.Since(t)
		guessWhoState.Unlock()
		client.SendServerMessage(fmt.Sprintf("Please wait %v before another Guess Who round.", remaining.Round(time.Second)))
		return
	}
	entry := characters[rand.Intn(len(characters))]
	round := &guessWhoRound{character: entry}
	guessWhoState.active[room] = round
	round.timeout = time.AfterFunc(guessWhoDuration, func() { guessWhoTimeUp(room, round) })
	guessWhoState.Unlock()
	opts.Emote = entry.Emote
	botSendIC(room, "Who am I?", entry.Sprite, "???", opts)
}

func guessWhoOnIC(client *Client, message string) {
	room := client.Area()
	if room == nil {
		return
	}
	opts := currentBotOptions()
	if !opts.GuessWho {
		return
	}
	guessWhoState.Lock()
	round := guessWhoState.active[room]
	if round == nil || !isCharacterAnswer(round.character, message) {
		guessWhoState.Unlock()
		return
	}
	delete(guessWhoState.active, room)
	guessWhoState.last[room] = time.Now()
	if round.timeout != nil {
		round.timeout.Stop()
	}
	answer := round.character.Answer
	name := client.OOCName()
	if name == "" {
		name = "Someone"
	}
	guessWhoState.Unlock()
	opts.Emote = round.character.Emote
	botSendIC(room, fmt.Sprintf("Correct, %s! I was %s. Goodbye!", name, answer), round.character.Sprite, botDisplayName, opts)
}

func guessWhoTimeUp(room *area.Area, round *guessWhoRound) {
	guessWhoState.Lock()
	if guessWhoState.active[room] != round {
		guessWhoState.Unlock()
		return
	}
	delete(guessWhoState.active, room)
	guessWhoState.last[room] = time.Now()
	guessWhoState.Unlock()
	opts := currentBotOptions()
	if opts.GuessWho {
		opts.Emote = round.character.Emote
		botSendIC(room, fmt.Sprintf("Nobody got it! I was %s. Goodbye!", round.character.Answer), round.character.Sprite, botDisplayName, opts)
	}
}
func cancelAllGuessWho() {
	guessWhoState.Lock()
	defer guessWhoState.Unlock()
	for room, round := range guessWhoState.active {
		if round.timeout != nil {
			round.timeout.Stop()
		}
		delete(guessWhoState.active, room)
	}
}
