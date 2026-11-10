// SPDX-License-Identifier: AGPL-3.0-or-later
package athena

var icebreakerLimiter = newBotLimiter()

func cmdIcebreaker(client *Client, _ []string, _ string) {
	opts := currentBotOptions()
	if !opts.Icebreaker {
		client.SendServerMessage("The icebreaker command is not enabled on this server.")
		return
	}
	room := client.Area()
	if room == nil {
		client.SendServerMessage("Please join a room first.")
		return
	}
	text := botRandomIcebreaker(currentBotContent().Icebreakers)
	if text == "" {
		client.SendServerMessage("No icebreaker questions are configured in icebreaker.txt.")
		return
	}
	if ok, reason := icebreakerLimiter.reserve(room); !ok {
		client.SendServerMessage(reason)
		return
	}
	go func() {
		defer icebreakerLimiter.release(room, true)
		botSendIC(room, "Icebreaker: "+text, botSprite(opts), botDisplayName, opts)
	}()
}
