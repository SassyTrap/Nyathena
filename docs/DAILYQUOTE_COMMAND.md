# Daily quote command

`/dailyquote` displays the date's quotation from `config/quote.txt` as IC text from Kai-Bot-SG. No HTTP service, API key, disk cache, or runtime internet connection is required.

Each non-comment line of `quote.txt` has the format `MM-DD | quotation — author`. The supplied sample has 365 entries. The same date always returns the same entry, based on the server's local calendar date. February 29 falls back to February 28 unless its own entry is added.

The `dailyquote = true` switch remains under `[Server]` in `config.toml`. A five-minute cooldown applies per room, shared with `/icebreaker`, with three simultaneous sessions maximum across both commands combined. `/reload` updates both the quotes and the safe bot settings without restarting the server.

See [BOT_LOCAL_COMMANDS.md](BOT_LOCAL_COMMANDS.md) for the shared bot appearance settings, `/icebreaker`, `/guesswho`, and deployment instructions.
