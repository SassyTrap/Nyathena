// SPDX-License-Identifier: AGPL-3.0-or-later
package athena

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	aolib "github.com/AO-Underground/aolib/go/v2"
	"github.com/MangosArentLiterature/Athena/internal/area"
	"github.com/MangosArentLiterature/Athena/internal/logger"
	"github.com/MangosArentLiterature/Athena/internal/settings"
)

const (
	dailyQuoteEndpoint    = "https://zenquotes.io/api/today"
	dailyQuoteBotName     = "Kai-Bot-SG"
	dailyQuoteCooldown    = 5 * time.Minute
	dailyQuoteMaxSessions = 3
	dailyQuoteMaxICRunes  = 250
	dailyQuotePace        = 1300 * time.Millisecond
	dailyQuoteHTTPTimeout = 8 * time.Second
	dailyQuoteCacheName   = "zenquotes_dailyquote_cache.json"
)

type zenDailyQuote struct {
	Quote  string `json:"q"`
	Author string `json:"a"`
}

type zenDailyQuoteDisk struct {
	Day  string        `json:"day"`
	Item zenDailyQuote `json:"item"`
}

var dailyQuoteSessions = struct {
	sync.Mutex
	busy   map[*area.Area]bool
	last   map[*area.Area]time.Time
	active int
}{busy: make(map[*area.Area]bool), last: make(map[*area.Area]time.Time)}

type zenDailyQuoteCache struct {
	sync.Mutex
	day  string
	item zenDailyQuote
}

var dailyQuoteCache zenDailyQuoteCache
var dailyQuoteHTTPClient = &http.Client{Timeout: dailyQuoteHTTPTimeout}

func dailyQuoteReserve(room *area.Area, now time.Time) (bool, string) {
	dailyQuoteSessions.Lock()
	defer dailyQuoteSessions.Unlock()
	if dailyQuoteSessions.busy[room] {
		return false, "Kai-Bot-SG is already speaking in this room."
	}
	if remaining := dailyQuoteCooldown - now.Sub(dailyQuoteSessions.last[room]); !dailyQuoteSessions.last[room].IsZero() && remaining > 0 {
		return false, fmt.Sprintf("This room can request another daily quote in %v.", remaining.Round(time.Second))
	}
	if dailyQuoteSessions.active >= dailyQuoteMaxSessions {
		return false, "Kai-Bot-SG is busy in other rooms. Try again shortly."
	}
	dailyQuoteSessions.busy[room] = true
	dailyQuoteSessions.active++
	return true, ""
}

func dailyQuoteRelease(room *area.Area, success bool, now time.Time) {
	dailyQuoteSessions.Lock()
	defer dailyQuoteSessions.Unlock()
	if !dailyQuoteSessions.busy[room] {
		return
	}
	delete(dailyQuoteSessions.busy, room)
	dailyQuoteSessions.active--
	if success {
		dailyQuoteSessions.last[room] = now
	}
}

func cmdDailyQuote(client *Client, _ []string, _ string) {
	if config == nil || !config.DailyQuote {
		client.SendServerMessage("The daily quote command is not enabled on this server.")
		return
	}
	room := client.Area()
	if room == nil {
		client.SendServerMessage("Please join a room before requesting a daily quote.")
		return
	}
	ok, reason := dailyQuoteReserve(room, time.Now())
	if !ok {
		client.SendServerMessage(reason)
		return
	}
	go func() {
		success := false
		defer func() { dailyQuoteRelease(room, success, time.Now()) }()

		ctx, cancel := context.WithTimeout(context.Background(), dailyQuoteHTTPTimeout)
		defer cancel()
		item, err := dailyQuoteCache.get(ctx, time.Now().UTC().Format("2006-01-02"), dailyQuoteEndpoint, dailyQuoteCachePath(), dailyQuoteHTTPClient, config.DailyQuoteOffline)
		if err != nil {
			logger.LogWarningf("dailyquote: ZenQuotes fetch failed: %v", err)
			client.SendServerMessage("Kai-Bot-SG couldn't load today's quote. Please try again later.")
			return
		}
		maxRunes := dailyQuoteMaxICRunes
		if config != nil && config.MaxMsg > 0 && config.MaxMsg < maxRunes {
			maxRunes = config.MaxMsg
		}
		if maxRunes < 32 {
			client.SendServerMessage("Kai-Bot-SG needs an IC message limit of at least 32 characters.")
			return
		}
		parts := splitDailyQuote(fmt.Sprintf("“%s” — %s", item.Quote, item.Author), maxRunes)
		if len(parts) == 0 {
			client.SendServerMessage("Kai-Bot-SG received an empty quote.")
			return
		}
		success = true
		speakerCharacter := dailyQuoteSprite()
		for i, part := range parts {
			if i > 0 {
				time.Sleep(dailyQuotePace)
			}
			// Synthetic IC packets use the unassigned character ID -1.
			broadcastToArea(room, &MSToClient{
				DeskModifier:           aolib.DeskModifierShown,
				Character:              encode(speakerCharacter),
				Emote:                  "normal",
				Message:                encode(part),
				Side:                   aolib.SideWit,
				EmoteModifier:          aolib.EmoteModifierNoPreanim,
				CharID:                 "-1",
				SfxDelay:               "0",
				Evidence:               "0",
				Realization:            "0",
				TextColor:              aolib.TextColorYellow,
				Showname:               encode(dailyQuoteBotName),
				PairedCharID:           "-1",
				NoninterruptingPreanim: "0",
				SfxLooping:             "0",
				Screenshake:            "0",
				Additive:               "0",
			})
		}
	}()
}

func dailyQuoteSprite() string {
	if getCharacterID("Phoenix") >= 0 {
		return "Phoenix"
	}
	chars := getCharacters()
	if len(chars) > 0 {
		return chars[0]
	}
	return ""
}

func dailyQuoteCachePath() string {
	if settings.ConfigPath == "" {
		return ""
	}
	return filepath.Join(settings.ConfigPath, dailyQuoteCacheName)
}

func (s *zenDailyQuoteCache) get(ctx context.Context, day, endpoint, path string, client *http.Client, offline bool) (zenDailyQuote, error) {
	if offline {
		return offlineDailyQuote(day), nil
	}
	s.Lock()
	defer s.Unlock()
	if day == s.day && validZenDailyQuote(s.item) {
		return s.item, nil
	}
	if path != "" {
		if b, err := os.ReadFile(path); err == nil && len(b) <= 4096 {
			var disk zenDailyQuoteDisk
			if json.Unmarshal(b, &disk) == nil && disk.Day == day && validZenDailyQuote(disk.Item) {
				s.day, s.item = day, disk.Item
				return s.item, nil
			}
		}
	}
	item, err := requestZenDailyQuote(ctx, endpoint, client)
	if err != nil {
		logger.LogWarningf("dailyquote: ZenQuotes unavailable, using built-in quote: %v", err)
		item = offlineDailyQuote(day)
	}
	s.day, s.item = day, item
	if path != "" {
		if err := persistZenDailyQuote(path, zenDailyQuoteDisk{Day: day, Item: item}); err != nil {
			logger.LogWarningf("dailyquote: couldn't persist cache: %v", err)
		}
	}
	return item, nil
}

var offlineDailyQuotes = []zenDailyQuote{
	{Quote: "A good question can be more useful than a quick answer.", Author: "Kai-Bot-SG"},
	{Quote: "The truth becomes clearer when every detail gets its turn.", Author: "Kai-Bot-SG"},
	{Quote: "Patience is often the first step toward a fair judgment.", Author: "Kai-Bot-SG"},
	{Quote: "Even the smallest clue deserves a careful look.", Author: "Kai-Bot-SG"},
	{Quote: "A strong case begins with listening.", Author: "Kai-Bot-SG"},
	{Quote: "Every story has another angle worth considering.", Author: "Kai-Bot-SG"},
	{Quote: "Good reasoning holds up when the questions get harder.", Author: "Kai-Bot-SG"},
	{Quote: "One clear fact can change the direction of a whole case.", Author: "Kai-Bot-SG"},
	{Quote: "The best argument leaves room for the facts to speak.", Author: "Kai-Bot-SG"},
	{Quote: "Clarity is built one careful observation at a time.", Author: "Kai-Bot-SG"},
	{Quote: "An open mind makes room for better evidence.", Author: "Kai-Bot-SG"},
	{Quote: "What matters is not how loudly a claim is made, but how well it holds up.", Author: "Kai-Bot-SG"},
}

func offlineDailyQuote(day string) zenDailyQuote {
	date, err := time.Parse("2006-01-02", day)
	if err != nil {
		return offlineDailyQuotes[0]
	}
	index := int(date.Unix()/86400) % len(offlineDailyQuotes)
	if index < 0 {
		index += len(offlineDailyQuotes)
	}
	return offlineDailyQuotes[index]
}

func validZenDailyQuote(q zenDailyQuote) bool {
	return q.Quote != "" && q.Author != "" && utf8.RuneCountInString(q.Quote)+utf8.RuneCountInString(q.Author) <= 3000
}

func requestZenDailyQuote(ctx context.Context, endpoint string, client *http.Client) (zenDailyQuote, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return zenDailyQuote{}, err
	}
	req.Header.Set("Accept", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return zenDailyQuote{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return zenDailyQuote{}, fmt.Errorf("ZenQuotes HTTP %d", res.StatusCode)
	}
	var items []zenDailyQuote
	if err := json.NewDecoder(io.LimitReader(res.Body, 32<<10)).Decode(&items); err != nil {
		return zenDailyQuote{}, fmt.Errorf("invalid ZenQuotes JSON: %w", err)
	}
	if len(items) == 0 {
		return zenDailyQuote{}, errors.New("empty ZenQuotes response")
	}
	item := zenDailyQuote{
		Quote:  strings.Join(strings.Fields(items[0].Quote), " "),
		Author: strings.Join(strings.Fields(items[0].Author), " "),
	}
	if !validZenDailyQuote(item) {
		return zenDailyQuote{}, errors.New("invalid or oversized quote")
	}
	return item, nil
}

func persistZenDailyQuote(path string, item zenDailyQuoteDisk) error {
	b, err := json.Marshal(item)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".dailyquote-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// splitDailyQuote prefers word boundaries and splits oversized words as needed.
func splitDailyQuote(text string, limit int) []string {
	if limit < 1 {
		return nil
	}
	words := strings.Fields(text)
	var output []string
	var line string
	for _, word := range words {
		runes := []rune(word)
		if line != "" && utf8.RuneCountInString(line)+1+len(runes) > limit {
			output = append(output, line)
			line = ""
		}
		if len(runes) > limit {
			for len(runes) > limit {
				output = append(output, string(runes[:limit]))
				runes = runes[limit:]
			}
			if len(runes) == 0 {
				continue
			}
			word = string(runes)
		}
		if line == "" {
			line = word
		} else {
			line += " " + word
		}
	}
	if line != "" {
		output = append(output, line)
	}
	return output
}
