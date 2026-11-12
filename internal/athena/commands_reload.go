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

import "fmt"

// cmdReload (/reload) hot-reloads every supported config/data file from disk
// without restarting the server. ADMIN only.
//
// Reloaded files (atomic, race-safe — see livereload.go):
//
//   - characters.txt     (append-only; insert/reorder/rename requires restart)
//   - music.txt
//   - cdns.txt
//   - backgrounds.txt
//   - parrot.txt
//   - 8ball.txt          (optional; missing file leaves current value intact)
//   - quote.txt, icebreakers.txt, guesswho.txt (local bot content)
//   - banned_words.txt   (only when automod is enabled)
//   - config.toml        (motd, description, raid guard and bot settings)
//
// Areas, listener ports, rate-limit windows, roles and the server name are NOT
// reloaded — those require a restart because they're snapshotted into other
// structures at startup.
func cmdReload(client *Client, _ []string, _ string) {
	summary, err := ReloadConfig()
	if err != nil {
		client.SendServerMessage(fmt.Sprintf("Reload failed: %v", err))
		addToBuffer(client, "CMD", fmt.Sprintf("/reload failed: %v", err), true)
		return
	}
	client.SendServerMessage(fmt.Sprintf("Reload OK: %s.", summary))
	addToBuffer(client, "CMD", fmt.Sprintf("/reload: %s", summary), true)
}
