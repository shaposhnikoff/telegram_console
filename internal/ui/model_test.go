package ui

import (
	"context"
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/shaposhnikoff/telegram_console/internal/app"
)

type fakeBackend struct {
	updates       chan app.Message
	states        chan string
	sends         int
	searchBefore  int
	contextTarget int
}

func (f *fakeBackend) Chats(context.Context) ([]app.Chat, error) {
	return []app.Chat{{ID: 1, Title: "Alice", CanSend: true}}, nil
}
func (f *fakeBackend) History(context.Context, int64, int) ([]app.Message, error) {
	return []app.Message{{ID: 2, ChatID: 1, Text: "hello"}}, nil
}
func (f *fakeBackend) Context(_ context.Context, _ int64, target int) ([]app.Message, error) {
	f.contextTarget = target
	return []app.Message{{ID: target, ChatID: 1, Text: "hit"}}, nil
}
func (f *fakeBackend) Search(_ context.Context, _ int64, _ string, before int) ([]app.Message, error) {
	f.searchBefore = before
	return nil, nil
}
func (f *fakeBackend) Send(context.Context, int64, string) error {
	f.sends++
	return errors.New("unknown result")
}
func (f *fakeBackend) MarkRead(context.Context, int64, int) error { return nil }
func (f *fakeBackend) Updates() <-chan app.Message                { return f.updates }
func (f *fakeBackend) ConnectionStates() <-chan string            { return f.states }

func TestMergeDeduplicatesAndSorts(t *testing.T) {
	got := merge([]app.Message{{ID: 3, Text: "old"}, {ID: 1}}, []app.Message{{ID: 3, Text: "new"}, {ID: 2}})
	if len(got) != 3 || got[0].ID != 1 || got[1].ID != 2 || got[2].Text != "new" {
		t.Fatalf("merge: %+v", got)
	}
}

func TestSendFailureDoesNotRetry(t *testing.T) {
	f := &fakeBackend{updates: make(chan app.Message)}
	m := New(context.Background(), f)
	m.active = 1
	m.mode = "compose"
	m.input.SetValue("message")
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("send command missing")
	}
	m = next.(Model)
	if _, ok := cmd().(sendMsg); !ok {
		t.Fatal("send result type")
	}
	next, cmd = m.Update(sendMsg{text: "message", err: errors.New("timeout")})
	m = next.(Model)
	if cmd != nil || f.sends != 1 || m.input.Value() != "message" {
		t.Fatalf("unexpected retry: sends=%d cmd=%v input=%q", f.sends, cmd, m.input.Value())
	}
}

func TestSearchPaginationAndContext(t *testing.T) {
	f := &fakeBackend{}
	m := New(context.Background(), f)
	m.active = 1
	m.mode = "results"
	m.query = "needle"
	m.results = []app.Message{{ID: 10, ChatID: 1, Text: "needle"}, {ID: 20, ChatID: 1, Text: "needle"}}
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'u'})
	if cmd == nil {
		t.Fatal("older search command missing")
	}
	m = next.(Model)
	if _, ok := cmd().(searchMsg); !ok || f.searchBefore != 10 {
		t.Fatalf("wrong search offset: %d", f.searchBefore)
	}
	m.busy = false
	next, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("context command missing")
	}
	m = next.(Model)
	if _, ok := cmd().(historyMsg); !ok || f.contextTarget != 10 || m.mode != "chat" {
		t.Fatalf("context target=%d mode=%s", f.contextTarget, m.mode)
	}
}

func TestSanitizeRemoteText(t *testing.T) {
	if got := singleLine("hello\x1b[2J\rworld"); got != "hello [2J world" {
		t.Fatalf("unsafe text: %q", got)
	}
}

func TestHistoryResultFromPreviousChatIsIgnored(t *testing.T) {
	m := New(context.Background(), &fakeBackend{})
	m.active = 2
	m.mode = "chat"
	m.messages = []app.Message{{ID: 5, ChatID: 2, Text: "current"}}
	next, cmd := m.Update(historyMsg{id: 1, messages: []app.Message{{ID: 9, ChatID: 1, Text: "stale"}}})
	m = next.(Model)
	if cmd != nil || len(m.messages) != 1 || m.messages[0].ChatID != 2 {
		t.Fatalf("stale history changed current chat: %+v", m.messages)
	}
}

func TestSearchResultFromPreviousChatIsIgnored(t *testing.T) {
	m := New(context.Background(), &fakeBackend{})
	m.active = 2
	m.mode = "chat"
	m.results = nil
	next, cmd := m.Update(searchMsg{messages: []app.Message{{ID: 9, ChatID: 1, Text: "stale"}}})
	m = next.(Model)
	if cmd != nil || m.mode != "chat" || len(m.results) != 0 {
		t.Fatalf("stale search changed current chat: mode=%s results=%+v", m.mode, m.results)
	}
}

func TestEmptySearchResultAfterLeavingChatIsIgnored(t *testing.T) {
	m := New(context.Background(), &fakeBackend{})
	m.active = 0
	m.mode = "list"
	next, cmd := m.Update(searchMsg{messages: nil})
	m = next.(Model)
	if cmd != nil || m.mode != "list" {
		t.Fatalf("stale empty search changed mode to %s", m.mode)
	}
}

func TestSendResultFromPreviousChatDoesNotAlterComposeDraft(t *testing.T) {
	m := New(context.Background(), &fakeBackend{})
	m.active = 2
	m.mode = "compose"
	m.input.SetValue("draft for chat 2")
	next, cmd := m.Update(sendMsg{text: "sent in chat 1", err: errors.New("timeout")})
	m = next.(Model)
	if cmd != nil || m.input.Value() != "draft for chat 2" {
		t.Fatalf("stale send replaced draft: %q", m.input.Value())
	}
}

func TestDuplicateUpdateDoesNotIncrementUnreadTwice(t *testing.T) {
	f := &fakeBackend{updates: make(chan app.Message)}
	m := New(context.Background(), f)
	m.chats = []app.Chat{{ID: 1, Unread: 0}}
	message := app.Message{ID: 7, ChatID: 1, Text: "hello"}
	for range 2 {
		next, _ := m.Update(updateMsg{message: message, ok: true})
		m = next.(Model)
	}
	if m.chats[0].Unread != 1 {
		t.Fatalf("duplicate update incremented unread to %d", m.chats[0].Unread)
	}
}

func TestHistoryResultFromEarlierOpeningOfSameChatIsIgnored(t *testing.T) {
	m := New(context.Background(), &fakeBackend{})
	m.active = 1
	m.mode = "chat"
	m.epoch = 2
	m.messages = []app.Message{{ID: 20, ChatID: 1, Text: "current"}}
	next, cmd := m.Update(historyMsg{id: 1, epoch: 1, messages: []app.Message{{ID: 10, ChatID: 1, Text: "stale"}}})
	m = next.(Model)
	if cmd != nil || len(m.messages) != 1 || m.messages[0].ID != 20 {
		t.Fatalf("earlier opening changed history: %+v", m.messages)
	}
}

func TestEarlierSearchQueryInSameChatIsIgnored(t *testing.T) {
	m := New(context.Background(), &fakeBackend{})
	m.active = 1
	m.mode = "chat"
	m.epoch = 2
	m.query = "second"
	next, cmd := m.Update(searchMsg{id: 1, epoch: 2, query: "first", messages: []app.Message{{ID: 10, ChatID: 1, Text: "first"}}})
	m = next.(Model)
	if cmd != nil || m.mode != "chat" || len(m.results) != 0 {
		t.Fatalf("earlier search changed current query: mode=%s results=%+v", m.mode, m.results)
	}
}
