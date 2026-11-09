# Daily quote command

Adds `/dailyquote` to Nyathena. Anyone can invoke it from OOC to display the daily ZenQuotes quotation in the current room as yellow IC text from **Kai-Bot-SG**.

The existing moderator `/quote` punishment is unchanged.

## Configuration

The command is disabled by default. Add this option under `[Server]` in the VPS's `config/config.toml` to enable it:

```toml
dailyquote = true
# Optional: use only built-in quotes; no HTTP requests or cache writes.
dailyquote_offline = false
```

Set `dailyquote = false` to disable `/dailyquote`. Set `dailyquote_offline = true` to use built-in quotes only. With offline mode disabled, ZenQuotes is tried first and a built-in quote is used if the API fails. When disabled, no quote requests are made. The sample `config_sample/config.toml` includes the option set to `true`; operators with existing configurations must add it manually. Restart Nyathena after changing this setting; it is not hot-reloaded.

## Behavior

- Uses the daily ZenQuotes API when available, falls back to a deterministic daily built-in quote if unavailable, and caches the result by UTC day in memory and in `zenquotes_dailyquote_cache.json` under the server config directory. Offline mode skips the API and disk cache entirely.
- Sends server-generated IC packets with no bot connection, player slot, or join/leave notification.
- Splits quote text into messages no longer than the smaller of 250 Unicode characters and the configured `max_message_length`, and pauses 1.3 seconds between messages.
- Limits requests to one active session per room and three sessions server-wide. A successful session starts a five-minute room cooldown. An API failure uses a built-in quote and starts the normal cooldown.
- Uses the `Phoenix` sprite if available, otherwise the first roster character. The selected character is displayed with the `normal` emote at the witness position.

## Deployment

Outbound HTTPS access to `zenquotes.io` is optional. If it is unavailable, the command uses bundled quotes. The config directory may be read-only; a cache write failure does not prevent the current session from working.

Check that the selected sprite and `normal` emote exist on the asset host. Verify the synthetic character ID `-1` displays correctly in the supported browser and desktop clients.

When using ZenQuotes, its free API requires visible attribution. Add a clickable credit to the deployed website or browser client, for example:

```html
<span>Quotes provided by <a href="https://zenquotes.io/">ZenQuotes API</a></span>
```

Adding attribution only to the server repository does not satisfy a visible-site requirement.

## Verification

Run `gofmt -w internal/athena/commands_dailyquote.go internal/athena/commands_dailyquote_test.go` and `go test ./...` against the complete Nyathena source tree. Test message display, long quotes, cooldowns, concurrent rooms, failed HTTP responses, offline mode, read-only cache directories, and cache reuse after a restart before deployment.

API documentation: https://docs.zenquotes.io/zenquotes-documentation/
