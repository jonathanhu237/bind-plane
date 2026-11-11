package release

import (
	"context"
	"encoding/json"
	"log"
	"time"
)

// The WeCom side of the chat: the shared core decides what to say; this draws it in WeCom's
// formats and sends it on the long connection.

// wecomCard draws a card as a WeCom template card: one with buttons is a button interaction card,
// one without is a text notice, which WeCom requires to lead somewhere when tapped.
func (b *WeComBot) wecomCard(card *chatCard) map[string]any {
	out := map[string]any{"main_title": map[string]string{"title": card.title, "desc": card.desc}, "task_id": card.taskID}
	if card.note != "" {
		out["sub_title_text"] = card.note
	}
	if len(card.facts) > 0 {
		facts := make([]map[string]string, 0, len(card.facts))
		for _, fact := range card.facts {
			facts = append(facts, map[string]string{"keyname": fact[0], "value": fact[1]})
		}
		out["horizontal_content_list"] = facts
	}
	if len(card.buttons) == 0 {
		out["card_type"] = "text_notice"
		out["card_action"] = map[string]any{"type": 1, "url": b.consoleLink(card.jobID)}
		return out
	}
	buttons := make([]map[string]any, 0, len(card.buttons))
	for _, button := range card.buttons {
		buttons = append(buttons, map[string]any{"text": button.text, "key": button.key})
	}
	out["card_type"] = "button_interaction"
	out["button_list"] = buttons
	return out
}

// reply answers a message in its chat: text goes out as a finished stream, which takes Markdown.
func (b *WeComBot) reply(ctx context.Context, reqID string, message chatMessage) {
	body := map[string]any{"msgtype": "stream", "stream": map[string]any{"id": wecomReqID("stream"), "finish": true, "content": message.text}}
	if message.card != nil {
		body = map[string]any{"msgtype": "template_card", "template_card": b.wecomCard(message.card)}
	}
	if err := b.send(ctx, wecomReply, reqID, body); err != nil && ctx.Err() == nil {
		log.Printf("wecom: reply: %v", err)
	}
}

func (b *WeComBot) onMessage(ctx context.Context, frame wecomFrame) {
	var cb wecomCallback
	if json.Unmarshal(frame.Body, &cb) != nil {
		return
	}
	chatType, chatID := cb.chat()
	in := chatIncoming{text: cb.Text.Content, isText: cb.MsgType == "text", sender: cb.From.UserID, accounts: []string{cb.From.UserID},
		place: chatPlace{kind: chatType, id: chatID}}
	b.reply(ctx, frame.Headers.ReqID, b.handleMessage(ctx, platformWeCom, in))
}

func (b *WeComBot) onEvent(ctx context.Context, frame wecomFrame, cb wecomCallback) {
	reqID := frame.Headers.ReqID
	switch cb.Event.EventType {
	case "enter_chat":
		if err := b.send(ctx, wecomWelcome, reqID, map[string]any{"msgtype": "text", "text": map[string]string{"content": chatHelp}}); err != nil && ctx.Err() == nil {
			log.Printf("wecom: welcome: %v", err)
		}
	case "template_card_event":
		taskID, key := cb.cardClick()
		outcome := b.handleClick(ctx, platformWeCom, chatClick{taskID: taskID, key: key, sender: cb.From.UserID, accounts: []string{cb.From.UserID}})
		if outcome == nil {
			return
		}
		// WeCom takes the new card only within five seconds of the click; a private one goes to
		// the member who clicked alone.
		body := map[string]any{"response_type": "update_template_card", "template_card": b.wecomCard(outcome.card)}
		if outcome.private {
			body["userids"] = []string{cb.From.UserID}
		}
		if err := b.send(ctx, wecomUpdateCard, reqID, body); err != nil && ctx.Err() == nil {
			log.Printf("wecom: update card: %v", err)
		}
	}
}

// notifyResults tells each chat how the jobs started from it ended. A result WeCom has not
// acknowledged is sent again later, at most five times.
func (b *WeComBot) notifyResults(ctx context.Context) {
	b.pendingMu.Lock()
	waiting := map[string]bool{}
	for reqID, sent := range b.pending {
		if time.Since(sent.sent) > 30*time.Second {
			delete(b.pending, reqID)
			continue
		}
		waiting[sent.jobID] = true
	}
	b.pendingMu.Unlock()
	for _, r := range b.pendingResults(ctx, platformWeCom, waiting) {
		body := map[string]any{"msgtype": "markdown", "markdown": map[string]string{"content": r.message.text}}
		if r.message.card != nil {
			body = map[string]any{"msgtype": "template_card", "template_card": b.wecomCard(r.message.card)}
		}
		body["chatid"] = r.place.id
		reqID := wecomReqID(wecomSend)
		b.pendingMu.Lock()
		b.pending[reqID] = wecomPending{jobID: r.jobID, sent: time.Now()}
		b.pendingMu.Unlock()
		if b.attempted(ctx, r.jobID) != nil {
			continue
		}
		if err := b.send(ctx, wecomSend, reqID, body); err != nil {
			return
		}
	}
}
