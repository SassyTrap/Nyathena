# Daily quote command

Adds `/dailyquote` to Nyathena. Anyone can invoke it from OOC to display the daily ZenQuotes quotation in the current room as yellow IC text from **Kai-Bot-SG**.

The existing moderator `/quote` punishment is unchanged.

## Behavior

- Fetches `https://zenquotes.io/api/today` and caches the result by UTC day in memory and in `zenquotes_dailyquote_cache.json` under the server config directory.
- Sends server-generated IC packets with no bot connection, player slot, or join/leave notification.
- Splits quote text into messages no longer than the smaller of 250 Unicode characters and the configured `max_message_length`, and pauses 1.3 seconds between messages.
- Limits requests to one active session per room and three sessions server-wide. A successful session starts a five-minute room cooldown. Failed fetches do not start the cooldown.
- Uses the `Phoenix` sprite if available, otherwise the first roster character. The selected character is displayed with the `normal` emote at the witness position.

## Deployment

The server requires outbound HTTPS access to `zenquotes.io`. The config directory must be writable for the disk cache; a cache write failure does not prevent the current session from working.

Check that the selected sprite and `normal` emote exist on the asset host. Verify the synthetic character ID `-1` displays correctly in the supported browser and desktop clients.

ZenQuotes requires visible attribution for the free API. Add a clickable credit to the deployed website or browser client, for example:

```html
<span>Quotes provided by <a href="https://zenquotes.io/">ZenQuotes API</a></span>
```

Adding attribution only to the server repository does not satisfy a visible-site requirement.

## Verification

Run `gofmt -w internal/athena/commands_dailyquote.go internal/athena/commands_dailyquote_test.go` and `go test ./...` against the complete Nyathena source tree. Test message display, long quotes, cooldowns, concurrent rooms, failed HTTP responses, and cache reuse after a restart before deployment.

API documentation: https://docs.zenquotes.io/zenquotes-documentation/
