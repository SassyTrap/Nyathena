# Local-file IC bot commands

No command contacts an external API. The bot sends server-generated IC packets, as before; no additional player account or separate process is required.

## Configuration

Put the following under `[Server]` in the deployed `config/config.toml`:

```toml
dailyquote = true
icebreaker = true
guesswho = true
bot_text_color = "yellow"
bot_ini_character = "hans christian andersen (fgo)"
bot_emote = "adult/normal"
```

The bot's `/dailyquote` and `/icebreaker` sprite uses the character-name override after selecting the first roster character as a fallback. `bot_emote` is the actual animation asset name (`adult/normal`), corresponding to the requested emote labelled `19 - normal` in that character's INI. This depends on the installed character assets matching those names. The position is witness. `bot_text_color` accepts `white`, `green`, `red`, `orange`, `blue`, `yellow`, `pink`, `cyan`, `grey`, or `rainbow` and applies to all three bot commands. If you prefer a different colour, edit it and use `/reload`.

## Text files

- `config/quote.txt`: `MM-DD | quotation — author`; the supplied 365 date-indexed lines are included. February 29 defaults to February 28 if missing. The date uses the server's local timezone.
- `config/icebreakers.txt`: one question per line, randomly chosen on each successful `/icebreaker`. The supplied 100 questions are included.
- `config/guesswho.txt`: `sprite-folder|animation|answer|accepted alias 1|accepted alias 2`. The default file is based on names from Nyathena's sample `characters.txt`, with common full-name aliases. Operators may add entries using character directories **verified to exist** on their chosen asset host. The `animation` field may differ between characters (the samples use `normal`); operators should choose the animation that exists for each sprite. The directory listing of `https://miku.pizza/base/` could not be verified, so the bundled examples do not claim to enumerate that site's entire roster.

Blank lines and comment lines starting with `#` are ignored. The files must be UTF-8 and cannot contain a literal `#` in an IC line, because it is the AO2 packet field delimiter. Invalid files are rejected by `/reload` rather than partly published.

## Commands

- `/dailyquote` — the same quote for every request on the same date; five-minute cooldown per room, maximum three simultaneous rooms.
- `/icebreaker` — a random question from the local file, not tied to the date; the same cooldown and three-session limit.
- `/guesswho` — displays the sprite from a random row in `guesswho.txt` at the witness position and asks "Who am I?". Players guess by sending **ordinary IC messages**, not OOC or commands. The answer is checked against the canonical name and aliases, ignoring punctuation, spaces and case; small spelling errors are accepted. A correct guess receives an IC reply naming the winner; after 60 seconds without a correct answer, the bot announces that no one succeeded and reveals the answer. There is a five-minute room cooldown.

The synthetic bot sends a goodbye and stops speaking when a round finishes. It is not a real network-connected player, so it does **not** trigger a literal player disconnect or clear every client's courtroom sprite. Actual join/leave would require an additional virtual-player implementation.

## Hot reload

An administrator can use `/reload` to reread the three data files and the bot configuration switches, text colour and animation settings. The new immutable file snapshot and setting snapshot are published safely for live connections. An active Guess Who round retains the character it began with; a newly started round uses the revised list. Disabling `guesswho` cancels rounds without a farewell message. A syntax error in a new file aborts the reload without swapping in that file's data. `characters.txt` retains the repository's existing append-only reload restriction.

Copy the supplied `config_sample` files into the **existing VPS config directory** (`config/`) when deploying. Files supplied in `config_sample` are examples and are not automatically read from that directory at runtime. The maintainer should verify that the Andersen sprite and `adult/normal` animation are available to the actual WebAO and desktop clients.

## Verification

Run the repository's build and test workflows, including `go test ./...` and `go test -race ./internal/athena` when the environment supports them. Test live `/reload`, IC guesses, both 60-second outcomes, quote date changes, and the selected character assets before deploying the public server.
