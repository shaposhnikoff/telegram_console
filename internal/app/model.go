package app

import "context"

type Chat struct {
	ID      int64
	Kind    string
	Title   string
	Unread  int
	TopID   int
	CanSend bool
}

type Message struct {
	ID     int
	ChatID int64
	Text   string
	Date   int
	Out    bool
}

// Backend keeps MTProto details out of the terminal event loop.
type Backend interface {
	Chats(context.Context) ([]Chat, error)
	History(context.Context, int64, int) ([]Message, error)
	Context(context.Context, int64, int) ([]Message, error)
	Search(context.Context, int64, string, int) ([]Message, error)
	Send(context.Context, int64, string) error
	MarkRead(context.Context, int64, int) error
	Updates() <-chan Message
	ConnectionStates() <-chan string
}
