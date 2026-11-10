// SPDX-License-Identifier: AGPL-3.0-or-later
package athena

import (
	"context"
	"encoding/json"
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
	icebreakerEndpoint    = "https://boneitis.org/today"
	icebreakerBotName     = "Kai-Bot-SG"
	icebreakerCooldown    = 5 * time.Minute
	icebreakerMaxSessions = 3
	icebreakerMaxICRunes  = 250
	icebreakerPace        = 1300 * time.Millisecond
	icebreakerHTTPTimeout = 8 * time.Second
	icebreakerCacheName   = "icebreaker_daily_cache.json"
)

type icebreakerResponse struct {
	Question string `json:"question"`
}

type icebreakerCacheFile struct {
	Day      string `json:"day"`
	Question string `json:"question"`
}

var icebreakerSessions = struct {
	sync.Mutex
	busy   map[*area.Area]bool
	last   map[*area.Area]time.Time
	active int
}{busy: make(map[*area.Area]bool), last: make(map[*area.Area]time.Time)}

type icebreakerQuestionCache struct {
	sync.Mutex
	day      string
	question string
}

var icebreakerCache icebreakerQuestionCache
var icebreakerHTTPClient = &http.Client{Timeout: icebreakerHTTPTimeout}

func icebreakerReserve(room *area.Area, now time.Time) (bool, string) {
	icebreakerSessions.Lock()
	defer icebreakerSessions.Unlock()
	if icebreakerSessions.busy[room] {
		return false, "Kai-Bot-SG is already asking an icebreaker in this room."
	}
	if last := icebreakerSessions.last[room]; !last.IsZero() {
		if remaining := icebreakerCooldown - now.Sub(last); remaining > 0 {
			return false, fmt.Sprintf("This room can request another icebreaker in %v.", remaining.Round(time.Second))
		}
	}
	if icebreakerSessions.active >= icebreakerMaxSessions {
		return false, "Kai-Bot-SG is busy in other rooms. Try again shortly."
	}
	icebreakerSessions.busy[room] = true
	icebreakerSessions.active++
	return true, ""
}

func icebreakerRelease(room *area.Area, success bool, now time.Time) {
	icebreakerSessions.Lock()
	defer icebreakerSessions.Unlock()
	if !icebreakerSessions.busy[room] {
		return
	}
	delete(icebreakerSessions.busy, room)
	icebreakerSessions.active--
	if success {
		icebreakerSessions.last[room] = now
	}
}

func cmdIcebreaker(client *Client, _ []string, _ string) {
	if config == nil || !config.Icebreaker {
		client.SendServerMessage("The icebreaker command is not enabled on this server.")
		return
	}
	room := client.Area()
	if room == nil {
		client.SendServerMessage("Please join a room before requesting an icebreaker.")
		return
	}
	ok, reason := icebreakerReserve(room, time.Now())
	if !ok {
		client.SendServerMessage(reason)
		return
	}
	go func() {
		success := false
		defer func() { icebreakerRelease(room, success, time.Now()) }()

		ctx, cancel := context.WithTimeout(context.Background(), icebreakerHTTPTimeout)
		defer cancel()
		day := time.Now().UTC().Format("2006-01-02")
		question := icebreakerCache.get(ctx, day, icebreakerEndpoint, icebreakerCachePath(), icebreakerHTTPClient, config.IcebreakerOffline)
		maxRunes := icebreakerMaxICRunes
		if config.MaxMsg > 0 && config.MaxMsg < maxRunes {
			maxRunes = config.MaxMsg
		}
		if maxRunes < 32 {
			client.SendServerMessage("Kai-Bot-SG needs an IC message limit of at least 32 characters.")
			return
		}
		parts := splitIcebreaker("Icebreaker: "+question, maxRunes)
		if len(parts) == 0 {
			client.SendServerMessage("Kai-Bot-SG couldn't load an icebreaker.")
			return
		}
		success = true
		character := icebreakerSprite()
		for i, part := range parts {
			if i > 0 {
				time.Sleep(icebreakerPace)
			}
			broadcastToArea(room, &MSToClient{
				DeskModifier:           aolib.DeskModifierShown,
				Character:              encode(character),
				Emote:                  "normal",
				Message:                encode(part),
				Side:                   aolib.SideWit,
				EmoteModifier:          aolib.EmoteModifierNoPreanim,
				CharID:                 "-1",
				SfxDelay:               "0",
				Evidence:               "0",
				Realization:            "0",
				TextColor:              aolib.TextColorYellow,
				Showname:               encode(icebreakerBotName),
				PairedCharID:           "-1",
				NoninterruptingPreanim: "0",
				SfxLooping:             "0",
				Screenshake:            "0",
				Additive:               "0",
			})
		}
	}()
}

func icebreakerSprite() string {
	chars := getCharacters()
	if len(chars) > 0 {
		return chars[0]
	}
	return ""
}

func icebreakerCachePath() string {
	if settings.ConfigPath == "" {
		return ""
	}
	return filepath.Join(settings.ConfigPath, icebreakerCacheName)
}

func (c *icebreakerQuestionCache) get(ctx context.Context, day, endpoint, path string, client *http.Client, offline bool) string {
	if offline {
		return offlineIcebreaker(day)
	}
	c.Lock()
	defer c.Unlock()
	if c.day == day && validIcebreaker(c.question) {
		return c.question
	}
	if path != "" {
		if data, err := os.ReadFile(path); err == nil && len(data) <= 4096 {
			var cached icebreakerCacheFile
			if json.Unmarshal(data, &cached) == nil && cached.Day == day && validIcebreaker(cached.Question) {
				c.day, c.question = day, cached.Question
				return c.question
			}
		}
	}
	question, err := requestIcebreaker(ctx, endpoint, client)
	if err != nil {
		logger.LogWarningf("icebreaker: Boneitis unavailable, using built-in question: %v", err)
		question = offlineIcebreaker(day)
	}
	c.day, c.question = day, question
	if path != "" {
		if err := persistIcebreaker(path, icebreakerCacheFile{Day: day, Question: question}); err != nil {
			logger.LogWarningf("icebreaker: couldn't persist cache: %v", err)
		}
	}
	return question
}

func requestIcebreaker(ctx context.Context, endpoint string, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Boneitis HTTP %d", res.StatusCode)
	}
	var response icebreakerResponse
	if err := json.NewDecoder(io.LimitReader(res.Body, 32<<10)).Decode(&response); err != nil {
		return "", fmt.Errorf("invalid Boneitis JSON: %w", err)
	}
	question := strings.Join(strings.Fields(response.Question), " ")
	if !validIcebreaker(question) {
		return "", fmt.Errorf("invalid or oversized icebreaker question")
	}
	return question, nil
}

func validIcebreaker(question string) bool {
	return question != "" && utf8.RuneCountInString(question) <= 3000
}

func persistIcebreaker(path string, item icebreakerCacheFile) error {
	data, err := json.Marshal(item)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".icebreaker-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

var offlineIcebreakers = []string{
	"What small thing always makes your day better?",
	"If you could master any skill overnight, what would it be?",
	"What's a place you'd love to visit and why?",
	"Which fictional world would you spend a weekend in?",
	"What game do you always come back to?",
	"What food reminds you of home?",
	"What's a hobby you've always wanted to try?",
	"If you could ask your future self one question, what would it be?",
	"What song always lifts your mood?",
	"What's your ideal way to spend a free afternoon?",
	"What is the best thing a game ever taught you?",
	"Which season best matches your personality?",
	"What ordinary item would you bring on an adventure?",
	"If you could relive one great meal, what would it be?",
	"What's something underrated that everyone should try?",
	"Which fictional character would make a great teammate?",
	"What would you name a tiny spaceship?",
	"What's the strangest skill you're proud of?",
	"What's the most memorable compliment you've received?",
	"What is one thing you could talk about for hours?",
}

func offlineIcebreaker(day string) string {
	date, err := time.Parse("2006-01-02", day)
	if err != nil {
		return offlineIcebreakers[0]
	}
	index := int(date.Unix()/86400) % len(offlineIcebreakers)
	if index < 0 {
		index += len(offlineIcebreakers)
	}
	return offlineIcebreakers[index]
}

func splitIcebreaker(text string, limit int) []string {
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
