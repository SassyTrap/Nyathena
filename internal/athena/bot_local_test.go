package athena

import (
	"strings"
	"testing"
	"time"

	"github.com/MangosArentLiterature/Athena/internal/settings"
)

func TestLocalQuoteCalendar(t *testing.T) {
	entries, err := parseQuoteLines([]string{"02-28 | February quote — Writer", "12-31 | December quote — Another"})
	if err != nil {
		t.Fatal(err)
	}
	if got := botQuoteForDate(&botContent{Quotes: entries}, time.Date(2028, 2, 29, 0, 0, 0, 0, time.UTC)); got != "February quote — Writer" {
		t.Fatalf("leap-day fallback: %s", got)
	}
	if got := botQuoteForDate(&botContent{Quotes: entries}, time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)); got != "December quote — Another" {
		t.Fatalf("date mismatch: %s", got)
	}
	if _, err := parseQuoteLines([]string{"02-30 | invalid"}); err == nil {
		t.Fatal("bad date was accepted")
	}
	if _, err := parseQuoteLines([]string{"01-01 | a", "01-01 | b"}); err == nil {
		t.Fatal("duplicate date was accepted")
	}
}
func TestGuessWhoAliasesAndTypos(t *testing.T) {
	entries, err := parseCharacterLines([]string{"Phoenix|normal|Phoenix Wright|Nick|Wright"})
	if err != nil {
		t.Fatal(err)
	}
	for _, guess := range []string{"PHOENIX WRIGHT", "phoenix wright!", "Nick", "I think it's Wright", "Pheonix Wright"} {
		if !isCharacterAnswer(entries[0], guess) {
			t.Fatalf("did not accept %q", guess)
		}
	}
	for _, guess := range []string{"", "Miles Edgeworth", "Maya", "Someone else", "not right"} {
		if isCharacterAnswer(entries[0], guess) {
			t.Fatalf("incorrectly accepted %q", guess)
		}
	}
}
func TestBotOptionsValidateColor(t *testing.T) {
	c := settings.DefaultConfig()
	c.BotTextColor = "cyan"
	c.BotIniCharacter = "hans christian andersen (fgo)"
	c.BotEmote = "adult/normal"
	x, err := parseBotOptions(c)
	if err != nil || string(x.Color) != "cyan" || x.Emote != "adult/normal" {
		t.Fatalf("options: %+v, %v", x, err)
	}
	c.BotTextColor = "invisible"
	if _, err := parseBotOptions(c); err == nil {
		t.Fatal("invalid color was allowed")
	}
}
func TestBotTextSplit(t *testing.T) {
	input := strings.Repeat("hello world ", 60) + "法廷 is open"
	parts := splitBotText(input, 37)
	if len(parts) < 2 {
		t.Fatal("expected several parts")
	}
	for _, p := range parts {
		if len([]rune(p)) > 37 {
			t.Fatalf("long part: %q", p)
		}
	}
	if strings.Join(parts, " ") != strings.Join(strings.Fields(input), " ") {
		t.Fatal("text changed")
	}
}
func TestQuestionSelection(t *testing.T) {
	if botRandomIcebreaker(nil) != "" {
		t.Fatal("empty list returned question")
	}
	a := []string{"one", "two", "three"}
	for i := 0; i < 40; i++ {
		out := botRandomIcebreaker(a)
		if out != "one" && out != "two" && out != "three" {
			t.Fatal("invalid question")
		}
	}
}
