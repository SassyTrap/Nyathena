// SPDX-License-Identifier: AGPL-3.0-or-later
package athena

import "time"

func cmdDailyQuote(client *Client, _ []string, _ string) {
	opts := currentBotOptions()
	if !opts.DailyQuote {
		client.SendServerMessage("The daily quote command is not enabled on this server.")
		return
	}
	room := client.Area()
	if room == nil {
		client.SendServerMessage("Please join a room first.")
		return
	}
	quote := botQuoteForDate(currentBotContent(), time.Now())
	if quote == "" {
		client.SendServerMessage("No quote is configured for today's date in quote.txt.")
		return
	}
	if ok, reason := botCommandLimiter.reserve(room); !ok {
		client.SendServerMessage(reason)
		return
	}
	go func() {
		defer botCommandLimiter.release(room, true)
		botSendIC(room, quote, botSprite(opts), botDisplayName, opts)
	}()
}
