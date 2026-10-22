package telegram

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"github.com/shaposhnikoff/telegram_console/internal/app"
)

type Backend struct {
	client  *telegram.Client
	mu      sync.RWMutex
	peers   map[int64]tg.InputPeerClass
	chats   map[int64]app.Chat
	updates chan app.Message
	states  chan string
}

func New(client *telegram.Client) *Backend {
	return &Backend{client: client, peers: make(map[int64]tg.InputPeerClass), chats: make(map[int64]app.Chat), updates: make(chan app.Message, 256), states: make(chan string, 16)}
}

func (b *Backend) Updates() <-chan app.Message     { return b.updates }
func (b *Backend) ConnectionStates() <-chan string { return b.states }
func (b *Backend) SetConnectionState(state telegram.ConnectionState) {
	b.SetStatus(state.String())
}
func (b *Backend) SetStatus(status string) {
	for {
		select {
		case b.states <- status:
			return
		default:
			select {
			case <-b.states:
			default:
			}
		}
	}
}

func (b *Backend) HandleUpdates(ctx context.Context, raw tg.UpdatesClass) error {
	var list []tg.UpdateClass
	switch u := raw.(type) {
	case *tg.Updates:
		list = u.Updates
	case *tg.UpdatesCombined:
		list = u.Updates
	case *tg.UpdateShort:
		list = []tg.UpdateClass{u.Update}
	}
	for _, item := range list {
		var rawMessage tg.MessageClass
		switch u := item.(type) {
		case *tg.UpdateNewMessage:
			rawMessage = u.Message
		case *tg.UpdateNewChannelMessage:
			rawMessage = u.Message
		}
		msg, ok := rawMessage.(*tg.Message)
		if !ok {
			continue
		}
		id := peerID(msg.PeerID)
		if id == 0 {
			continue
		}
		select {
		case b.updates <- toMessage(msg, id):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (b *Backend) Chats(ctx context.Context) ([]app.Chat, error) {
	out := make([]app.Chat, 0, 100)
	peers := make(map[int64]tg.InputPeerClass)
	chats := make(map[int64]app.Chat)
	req := &tg.MessagesGetDialogsRequest{OffsetPeer: &tg.InputPeerEmpty{}, Limit: 100}
	var previous int64
	complete := false
	for page := 0; page < 1000; page++ {
		result, err := b.client.API().MessagesGetDialogs(ctx, req)
		if err != nil {
			return nil, err
		}
		var dialogs []tg.DialogClass
		var users []tg.UserClass
		var rawChats []tg.ChatClass
		var rawMessages []tg.MessageClass
		count := 0
		switch v := result.(type) {
		case *tg.MessagesDialogs:
			dialogs, users, rawChats, rawMessages = v.Dialogs, v.Users, v.Chats, v.Messages
		case *tg.MessagesDialogsSlice:
			dialogs, users, rawChats, rawMessages, count = v.Dialogs, v.Users, v.Chats, v.Messages, v.Count
		default:
			return nil, fmt.Errorf("unsupported dialogs response %T", result)
		}
		userMap := make(map[int64]*tg.User)
		chatMap := make(map[int64]tg.ChatClass)
		for _, raw := range users {
			if u, ok := raw.(*tg.User); ok {
				userMap[u.ID] = u
			}
		}
		for _, raw := range rawChats {
			switch c := raw.(type) {
			case *tg.Chat:
				chatMap[c.ID] = c
			case *tg.Channel:
				chatMap[c.ID] = c
			}
		}
		lastID, lastTop := int64(0), 0
		for _, raw := range dialogs {
			d, ok := raw.(*tg.Dialog)
			if !ok {
				continue
			}
			id := peerID(d.Peer)
			if id == 0 {
				continue
			}
			c := app.Chat{ID: id, Unread: d.UnreadCount, TopID: d.TopMessage, CanSend: true}
			var peer tg.InputPeerClass
			switch p := d.Peer.(type) {
			case *tg.PeerUser:
				u := userMap[p.UserID]
				if u == nil {
					continue
				}
				c.Kind, c.Title = "user", strings.TrimSpace(u.FirstName+" "+u.LastName)
				if c.Title == "" {
					c.Title = u.Username
				}
				if u.Self {
					peer = &tg.InputPeerSelf{}
				} else {
					peer = u.AsInputPeer()
				}
			case *tg.PeerChat:
				v, ok := chatMap[p.ChatID].(*tg.Chat)
				if !ok {
					continue
				}
				c.Kind, c.Title = "group", v.Title
				peer = &tg.InputPeerChat{ChatID: p.ChatID}
			case *tg.PeerChannel:
				v, ok := chatMap[p.ChannelID].(*tg.Channel)
				if !ok {
					continue
				}
				if v.Broadcast {
					rights, ok := v.GetAdminRights()
					c.Kind = "channel"
					c.CanSend = v.Creator || (ok && rights.PostMessages)
				} else {
					c.Kind = "group"
				}
				c.Title = v.Title
				peer = v.AsInputPeer()
			}
			if peer == nil {
				continue
			}
			lastID, lastTop = id, d.TopMessage
			if _, seen := chats[id]; !seen {
				out = append(out, c)
			}
			peers[id], chats[id] = peer, c
		}
		if len(dialogs) < req.Limit || lastID == 0 || len(out) >= count && count > 0 {
			complete = true
			break
		}
		if lastID == previous {
			return nil, errors.New("dialog pagination made no progress")
		}
		previous = lastID
		req.OffsetPeer = peers[lastID]
		req.OffsetID = lastTop
		req.OffsetDate = 0
		for _, raw := range rawMessages {
			switch msg := raw.(type) {
			case *tg.Message:
				if msg.ID == lastTop && peerID(msg.PeerID) == lastID {
					req.OffsetDate = msg.Date
				}
			case *tg.MessageService:
				if msg.ID == lastTop && peerID(msg.PeerID) == lastID {
					req.OffsetDate = msg.Date
				}
			}
			if req.OffsetDate != 0 {
				break
			}
		}
	}
	if !complete {
		return nil, errors.New("dialog pagination limit reached")
	}
	b.mu.Lock()
	b.peers, b.chats = peers, chats
	b.mu.Unlock()
	return out, nil
}

func (b *Backend) peer(id int64) (tg.InputPeerClass, app.Chat, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	p, ok := b.peers[id]
	if !ok {
		return nil, app.Chat{}, errors.New("chat is not loaded")
	}
	return p, b.chats[id], nil
}

func (b *Backend) History(ctx context.Context, id int64, before int) ([]app.Message, error) {
	p, _, err := b.peer(id)
	if err != nil {
		return nil, err
	}
	raw, err := b.client.API().MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{Peer: p, OffsetID: before, Limit: 50})
	if err != nil {
		return nil, err
	}
	return unpack(raw, id)
}

func (b *Backend) Context(ctx context.Context, id int64, target int) ([]app.Message, error) {
	p, _, err := b.peer(id)
	if err != nil {
		return nil, err
	}
	raw, err := b.client.API().MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{Peer: p, OffsetID: target, AddOffset: -20, Limit: 40})
	if err != nil {
		return nil, err
	}
	return unpack(raw, id)
}

func (b *Backend) Search(ctx context.Context, id int64, q string, before int) ([]app.Message, error) {
	p, _, err := b.peer(id)
	if err != nil {
		return nil, err
	}
	raw, err := b.client.API().MessagesSearch(ctx, &tg.MessagesSearchRequest{Peer: p, Q: q, Filter: &tg.InputMessagesFilterEmpty{}, OffsetID: before, Limit: 50})
	if err != nil {
		return nil, err
	}
	return unpack(raw, id)
}

func (b *Backend) Send(ctx context.Context, id int64, text string) error {
	p, chat, err := b.peer(id)
	if err != nil {
		return err
	}
	if !chat.CanSend {
		return errors.New("posting is not permitted in this channel")
	}
	if strings.TrimSpace(text) == "" {
		return errors.New("message is empty")
	}
	randomID, err := b.client.RandInt64()
	if err != nil {
		return err
	}
	return b.client.SendMessage(ctx, &tg.MessagesSendMessageRequest{Peer: p, Message: text, RandomID: randomID})
}

func (b *Backend) MarkRead(ctx context.Context, id int64, maxID int) error {
	p, _, err := b.peer(id)
	if err != nil {
		return err
	}
	if maxID <= 0 {
		return nil
	}
	if channel, ok := p.(*tg.InputPeerChannel); ok {
		_, err = b.client.API().ChannelsReadHistory(ctx, &tg.ChannelsReadHistoryRequest{Channel: &tg.InputChannel{ChannelID: channel.ChannelID, AccessHash: channel.AccessHash}, MaxID: maxID})
	} else {
		_, err = b.client.API().MessagesReadHistory(ctx, &tg.MessagesReadHistoryRequest{Peer: p, MaxID: maxID})
	}
	return err
}

func unpack(raw tg.MessagesMessagesClass, id int64) ([]app.Message, error) {
	var source []tg.MessageClass
	switch v := raw.(type) {
	case *tg.MessagesMessages:
		source = v.Messages
	case *tg.MessagesMessagesSlice:
		source = v.Messages
	case *tg.MessagesChannelMessages:
		source = v.Messages
	default:
		return nil, fmt.Errorf("unsupported messages response %T", raw)
	}
	out := make([]app.Message, 0, len(source))
	for _, item := range source {
		switch m := item.(type) {
		case *tg.Message:
			out = append(out, toMessage(m, id))
		case *tg.MessageService:
			out = append(out, app.Message{ID: m.ID, ChatID: id, Text: "[service event]", Date: m.Date, Out: m.Out})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func toMessage(m *tg.Message, chatID int64) app.Message {
	text := m.Message
	if text == "" {
		text = "[media or service content]"
	}
	return app.Message{ID: m.ID, ChatID: chatID, Text: text, Date: m.Date, Out: m.Out}
}

func peerID(raw tg.PeerClass) int64 {
	switch p := raw.(type) {
	case *tg.PeerUser:
		return p.UserID
	case *tg.PeerChat:
		return -p.ChatID
	case *tg.PeerChannel:
		return -p.ChannelID - (1 << 40)
	}
	return 0
}

var _ app.Backend = (*Backend)(nil)
