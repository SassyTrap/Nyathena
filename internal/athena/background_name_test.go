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
	"os"
	"strings"
	"testing"
)

// /bg no longer looks the name up in backgrounds.txt, so the validator is the
// only thing standing between a player and every other client in the area.
// Each rejected case is a real filename-resolution or protocol trick.
func TestBackgroundNameProblem(t *testing.T) {
	accepted := []string{
		"gs4-court",
		"Custom Court (night)",
		"mybg.png",
		"local_file-01",
		"ao2 background",
		"背景",
		strings.Repeat("a", maxBackgroundNameLen),
	}
	for _, name := range accepted {
		if p := backgroundNameProblem(name); p != "" {
			t.Errorf("backgroundNameProblem(%q) = %q, want accepted", name, p)
		}
	}

	rejected := map[string]string{
		"":    "empty name",
		"   ": "whitespace only",
		strings.Repeat("a", maxBackgroundNameLen+1): "over the length cap",
		"../secrets":                     "leading traversal",
		"bg/../../characters/characters": "embedded traversal",
		"sub/bg":                         "path separator",
		`sub\bg`:                         "windows path separator",
		"http:bg":                        "scheme separator",
		"bg#1":                           "field delimiter",
		"bg%":                            "packet terminator",
		"bg&1":                           "sub-field delimiter",
		"bg$1":                           "dollar delimiter",
		"bg\n":                           "newline",
		"bg\t1":                          "tab",
		"bg\x00":                         "nul",
		".hidden":                        "leading dot",
		"~backup":                        "leading tilde",
		"bg.":                            "trailing dot",
		"bg*":                            "wildcard",
		"bg?1":                           "wildcard",
		"bg|1":                           "pipe",
		"bg<1":                           "redirection",
	}
	for name, why := range rejected {
		if p := backgroundNameProblem(name); p == "" {
			t.Errorf("backgroundNameProblem(%q) accepted a %v", name, why)
		}
	}
}

// The validator is only worth anything if cmdBg runs it before the name reaches
// area state and the area broadcast. Asserted against the source for the same
// reason TestBroadcastingOOCCommandsConsultTheGate is: it is an ordering
// property, and it regresses by nothing worse than an edit that moves the check
// below the send.
func TestCmdBgValidatesBeforeBroadcast(t *testing.T) {
	src, err := os.ReadFile("commands_area_admin.go")
	if err != nil {
		t.Fatalf("read commands_area_admin.go: %v", err)
	}
	text := string(src)

	start := strings.Index(text, "func cmdBg(")
	if start < 0 {
		t.Fatal("cmdBg not found")
	}
	body := text[start:]
	if end := strings.Index(body, "\nfunc "); end > 0 {
		body = body[:end]
	}

	check := strings.Index(body, "backgroundNameProblem(arg)")
	if check < 0 {
		t.Fatal("cmdBg no longer calls backgroundNameProblem; an arbitrary name " +
			"would be stored in area state and broadcast unvalidated")
	}
	for _, landmark := range []string{"SetBackground(", "broadcastToArea(", "addToBuffer("} {
		if loc := strings.Index(body, landmark); loc >= 0 && loc < check {
			t.Errorf("cmdBg reaches %v at offset %d, before the validator at %d",
				landmark, loc, check)
		}
	}
	if !strings.Contains(body[check:], "return") {
		t.Error("cmdBg no longer returns after a rejected name; the invalid name " +
			"would still be applied")
	}
}
