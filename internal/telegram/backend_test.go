package telegram

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gotd/td/tg"
	"github.com/shaposhnikoff/telegram_console/internal/app"
)

func TestPeerIDNamespaces(t *testing.T) {
	ids := []int64{
		peerID(&tg.PeerUser{UserID: 42}),
		peerID(&tg.PeerChat{ChatID: 42}),
		peerID(&tg.PeerChannel{ChannelID: 42}),
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		if id == 0 || seen[id] {
			t.Fatalf("peer ID collision: %v", ids)
		}
		seen[id] = true
	}
}

func TestUnpackOrdersMessagesAndShowsMediaPlaceholder(t *testing.T) {
	raw := &tg.MessagesMessages{Messages: []tg.MessageClass{
		&tg.Message{ID: 3, Message: "new"},
		&tg.Message{ID: 1},
	}}
	got, err := unpack(raw, 99)
	if err != nil || len(got) != 2 || got[0].ID != 1 || got[0].Text == "" || got[1].ID != 3 {
		t.Fatalf("unpack: %+v, %v", got, err)
	}
}

func TestServiceOnlyPageKeepsPaginationCursor(t *testing.T) {
	raw := &tg.MessagesMessages{Messages: []tg.MessageClass{
		&tg.MessageService{ID: 20, Date: 1},
		&tg.MessageService{ID: 19, Date: 1},
	}}
	got, err := unpack(raw, 99)
	if err != nil || len(got) != 2 || got[0].ID != 19 || got[1].ID != 20 {
		t.Fatalf("service page lost cursor: %+v, %v", got, err)
	}
}

func TestUpdateBurstDeliversAllMessagesInOrder(t *testing.T) {
	b := New(nil)
	count := cap(b.updates) + 10
	raw := &tg.Updates{Updates: make([]tg.UpdateClass, count)}
	for i := range raw.Updates {
		raw.Updates[i] = &tg.UpdateNewMessage{Message: &tg.Message{ID: i + 1, PeerID: &tg.PeerUser{UserID: 42}}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- b.HandleUpdates(ctx, raw) }()
	for want := 1; want <= count; want++ {
		select {
		case msg := <-b.Updates():
			if msg.ID != want || msg.ChatID != 42 {
				t.Fatalf("update %d: %+v", want, msg)
			}
		case <-ctx.Done():
			t.Fatal("timed out draining update burst")
		}
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("producer did not finish")
	}
}

func TestFullUpdateQueueStopsOnCancellation(t *testing.T) {
	b := New(nil)
	for i := 0; i < cap(b.updates); i++ {
		b.updates <- app.Message{ID: i + 1}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	raw := &tg.Updates{Updates: []tg.UpdateClass{
		&tg.UpdateNewMessage{Message: &tg.Message{ID: 999, PeerID: &tg.PeerUser{UserID: 42}}},
	}}
	if err := b.HandleUpdates(ctx, raw); !errors.Is(err, context.Canceled) {
		t.Fatalf("blocked producer error: %v", err)
	}
}
