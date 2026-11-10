# Icebreaker command

The public `/icebreaker` OOC command posts the daily question from the [Boneitis Question of the Day API](https://boneitis.org/docs) as yellow IC text from Kai-Bot-SG in the caller's current room. It does not create a player connection or send join/leave messages.

## Configuration

Add these options under `[Server]` in `config/config.toml`:

```toml
icebreaker = true
icebreaker_offline = false
```

`icebreaker = false` disables the command, including when the option is absent. Setting `icebreaker_offline = true` uses bundled questions and makes no HTTP requests or cache writes. Changes take effect after a server restart; they are not hot-reloaded.

## Behavior

- Requests `https://boneitis.org/today` (the `general` category by default), reads the JSON `question` field, and caches it for the UTC day in memory and, when possible, in `icebreaker_daily_cache.json` inside the server's config directory.
- Uses a built-in daily question if the API is unreachable or returns an invalid response. No network access or writable cache directory is required for the fallback.
- Limits requests to three active icebreaker sessions server-wide and one per room. A completed message starts a five-minute cooldown for that room.
- Uses the first character from the server roster with the `normal` emote in the witness position, matching the current `/dailyquote` implementation. The message is split into chunks no longer than the smaller of 250 characters and `max_message_length`, paced 1.3 seconds apart.
- Keeps `/dailyquote` fully independent, including its own configuration and cooldown.

## Deployment

Build the updated server, add `icebreaker = true` to the deployed `config/config.toml`, and restart the existing Nyathena service. The sample config has the switch disabled by default. Test `/icebreaker` in the target browser and desktop clients and verify the selected character's `normal` emote exists.

Run `go test ./...` and the repository's normal build checks before deploying. The API response schema is documented at https://boneitis.org/docs.
