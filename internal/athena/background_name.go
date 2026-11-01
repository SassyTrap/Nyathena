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
	"unicode"
	"unicode/utf8"
)

// maxBackgroundNameLen caps a background name in characters. Every name in the
// shipped background lists is far shorter than this; the cap exists so one
// player cannot park a multi-kilobyte string in area state, which is re-sent to
// every client on every area join and written to the OOC buffer.
const maxBackgroundNameLen = 128

// backgroundNameProblem reports why name cannot be used as an area background,
// or "" when it is acceptable. The caller is expected to have trimmed it.
//
// /bg used to accept only names from backgrounds.txt. It now accepts any name,
// because a client that ships its own art (local background files, a private
// asset folder) can display art the server has never heard of -- the "any
// background name" freedom KFO-style servers give. That turns the name into
// player-controlled input on a path that reaches every other client, so it is
// validated instead of looked up.
//
// What is refused, and why:
//
//   - Empty / whitespace-only: there is no file to load.
//   - '#', '%', '&', '$': AO2's field and packet delimiters, and the sub-field
//     delimiters of the packets that carry area info. escapeOutgoing()
//     neutralises them on the wire, but the escaped form ("a<num>b") no longer
//     names the file the player meant, so the honest answer is to refuse it
//     rather than silently mangle it.
//   - Control characters: never part of a filename, and they corrupt the
//     client's own name handling and the OOC buffer.
//   - '/', '\', ':', '*', '?', '"', '<', '>', '|': the name is a filename on
//     the CLIENT, resolved under its background folder. A separator escapes
//     that folder, and ':' turns the name into a URL for clients that resolve
//     backgrounds through the asset server.
//   - "..", a leading '.' or '~', a trailing '.' or space: path traversal and
//     the usual tricks for resolving a different file than the one written
//     down.
//   - More than maxBackgroundNameLen characters.
func backgroundNameProblem(name string) string {
	if strings.TrimSpace(name) == "" {
		return "Invalid background. Usage: /bg <background>"
	}
	if utf8.RuneCountInString(name) > maxBackgroundNameLen {
		return fmt.Sprintf("Invalid background: names are limited to %v characters.", maxBackgroundNameLen)
	}
	if strings.Contains(name, "..") {
		return "Invalid background: '..' is not allowed."
	}
	for _, r := range name {
		switch r {
		case '#', '%', '&', '$':
			return "Invalid background: '#', '%', '&' and '$' are reserved by the AO2 protocol."
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return "Invalid background: path and filename characters are not allowed."
		}
		if unicode.IsControl(r) {
			return "Invalid background: control characters are not allowed."
		}
	}
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "~") {
		return "Invalid background: the name cannot start with '.' or '~'."
	}
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return "Invalid background: the name cannot end with '.' or a space."
	}
	return ""
}
