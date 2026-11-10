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
	"sync"

	"github.com/MangosArentLiterature/Athena/internal/logger"
	"github.com/MangosArentLiterature/Athena/internal/settings"
)

// hotconfig holds the small whitelist of server-config fields that are safe to
// reload at runtime without restarting the server. Only fields that are read
// under per-connection goroutines AND whose change cannot desync client state
// belong here. Anything that is snapshotted into preallocated packets, caches,
// rate-limit windows, or area structs is deliberately excluded because
// changing it mid-session leaves the server inconsistent.
//
// Currently whitelisted:
//   - Motd            : shown to clients when they finish login
//   - Desc            : shown in the PN (player-count) packet on initial handshake
//   - RaidGuardEnabled: the raid guard's master switch (raidGuardActive, see
//     raidguard.go) -- a plain atomic.Bool gate read at the
//     top of every hot-path hook, not cached here like
//     Motd/Desc. Safe to flip live for the same reason
//     those are: it can never desync a connected client,
//     it only changes whether new traffic gets scored.
//
// Explicitly NOT whitelisted (would require careful, invasive work):
//   - Name        : baked into serverName and every server-to-client CT
//   - Port/Addr   : listener already bound
//   - Areas/*     : would invalidate AreaData snapshots and pre-built SM packet
//   - Rate limits : precomputed into time.Duration globals at startup
//   - max_players : clients may already be connected above any lowered cap
var (
	hotConfigMu sync.RWMutex
	hotMotd     string
	hotDesc     string
)

// initHotConfig seeds the hot-reload cache from the initial configuration.
// Called once from InitServer after the config is loaded.
func initHotConfig(c *settings.Config) {
	hotConfigMu.Lock()
	defer hotConfigMu.Unlock()
	hotMotd = c.Motd
	hotDesc = c.Desc
	if _, err := publishBotOptions(c); err != nil {
		logger.LogWarningf("bot: %v", err)
	}
}

// GetMotd returns the server's current message-of-the-day under a read lock.
// Safe to call from any goroutine.
func GetMotd() string {
	hotConfigMu.RLock()
	defer hotConfigMu.RUnlock()
	return hotMotd
}

// GetServerDesc returns the server's current public description under a read
// lock. Safe to call from any goroutine.
func GetServerDesc() string {
	hotConfigMu.RLock()
	defer hotConfigMu.RUnlock()
	return hotDesc
}

// ReloadHotConfig re-reads config.toml from disk and applies the whitelist of
// fields into the hot cache. All other fields in the file are ignored — they
// still reflect the values from server startup. Returns the number of fields
// that actually changed so callers can log a summary.
//
// Safe to invoke from SIGHUP handlers or the stdin CLI.
func ReloadHotConfig() (int, error) {
	conf, err := settings.GetConfig()
	if err != nil {
		return 0, err
	}
	changed := 0
	hotConfigMu.Lock()
	if hotMotd != conf.Motd {
		hotMotd = conf.Motd
		changed++
	}
	if hotDesc != conf.Desc {
		hotDesc = conf.Desc
		changed++
	}
	hotConfigMu.Unlock()

	// Raid guard's on/off switch isn't part of the hotConfigMu-guarded cache
	// above -- it already lives behind its own atomic.Bool (raidGuardActive,
	// raidguard.go) that every hot-path hook reads directly -- so it's synced
	// here as a plain compare-and-store rather than through hotMotd/hotDesc's
	// pattern.
	if raidGuardActive.Load() != conf.RaidGuardEnabled {
		raidGuardActive.Store(conf.RaidGuardEnabled)
		changed++
	}

	if n, err := publishBotOptions(conf); err != nil {
		return changed, err
	} else {
		changed += n
	}

	if changed > 0 {
		logger.LogInfof("hot-reload: applied %d changed field(s) from config.toml", changed)
	} else {
		logger.LogInfo("hot-reload: no whitelisted fields changed")
	}
	return changed, nil
}
