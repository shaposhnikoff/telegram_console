package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/shaposhnikoff/telegram_console/internal/app"
)

type chatsMsg struct {
	chats []app.Chat
	err   error
}
type historyMsg struct {
	id       int64
	epoch    uint64
	messages []app.Message
	older    bool
	target   int
	err      error
}
type searchMsg struct {
	id       int64
	epoch    uint64
	query    string
	messages []app.Message
	older    bool
	err      error
}
type sendMsg struct {
	id    int64
	epoch uint64
	text  string
	err   error
}
type readMsg struct {
	id    int64
	epoch uint64
	err   error
}
type updateMsg struct {
	message app.Message
	ok      bool
}
type stateMsg struct {
	state string
	ok    bool
}

type Model struct {
	backend     app.Backend
	ctx         context.Context
	chats       []app.Chat
	selected    int
	active      int64
	messages    []app.Message
	results     []app.Message
	query       string
	resultIndex int
	mode        string
	input       textinput.Model
	status      string
	busy        bool
	sending     bool
	width       int
	height      int
	scroll      int
	seenReady   bool
	seenUnread  map[int64]map[int]struct{}
	epoch       uint64
}

func New(ctx context.Context, backend app.Backend) Model {
	in := textinput.New()
	in.Prompt = "> "
	in.CharLimit = 4096
	return Model{backend: backend, ctx: ctx, mode: "list", input: in, status: "Loading chats...", width: 80, height: 24}
}

func (m Model) Init() tea.Cmd { return tea.Batch(m.loadChats(), m.listen(), m.listenState()) }

func (m Model) loadChats() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 20*time.Second)
		defer cancel()
		v, err := m.backend.Chats(ctx)
		return chatsMsg{v, err}
	}
}

func (m Model) loadHistory(id int64, before int, older bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 20*time.Second)
		defer cancel()
		v, err := m.backend.History(ctx, id, before)
		return historyMsg{id: id, epoch: m.epoch, messages: v, older: older, err: err}
	}
}

func (m Model) loadContext(id int64, target int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 20*time.Second)
		defer cancel()
		v, err := m.backend.Context(ctx, id, target)
		return historyMsg{id: id, epoch: m.epoch, messages: v, target: target, err: err}
	}
}

func (m Model) search(id int64, q string, before int, older bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 20*time.Second)
		defer cancel()
		v, err := m.backend.Search(ctx, id, q, before)
		return searchMsg{id: id, epoch: m.epoch, query: q, messages: v, older: older, err: err}
	}
}

func (m Model) send(id int64, body string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
		defer cancel()
		return sendMsg{id: id, epoch: m.epoch, text: body, err: m.backend.Send(ctx, id, body)}
	}
}

func (m Model) markRead(id int64, maxID int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 15*time.Second)
		defer cancel()
		return readMsg{id: id, epoch: m.epoch, err: m.backend.MarkRead(ctx, id, maxID)}
	}
}

func (m Model) listen() tea.Cmd {
	return func() tea.Msg {
		select {
		case v, ok := <-m.backend.Updates():
			return updateMsg{v, ok}
		case <-m.ctx.Done():
			return updateMsg{ok: false}
		}
	}
}
func (m Model) listenState() tea.Cmd {
	return func() tea.Msg {
		select {
		case v, ok := <-m.backend.ConnectionStates():
			return stateMsg{v, ok}
		case <-m.ctx.Done():
			return stateMsg{ok: false}
		}
	}
}

func (m Model) Update(raw tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := raw.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(max(10, msg.Width-4))
	case chatsMsg:
		m.busy = false
		if msg.err != nil {
			m.status = "Chat loading failed: " + msg.err.Error()
		} else {
			m.chats = msg.chats
			m.status = fmt.Sprintf("%d chats loaded", len(msg.chats))
			if m.selected >= len(m.chats) {
				m.selected = 0
			}
		}
	case historyMsg:
		if msg.id != m.active || (msg.epoch != 0 && msg.epoch != m.epoch) {
			break
		}
		m.busy = false
		if msg.err != nil {
			m.status = "History failed: " + msg.err.Error()
		} else {
			m.messages = merge(m.messages, msg.messages)
			if msg.target != 0 {
				for i, v := range m.messages {
					if v.ID == msg.target {
						m.scroll = max(0, len(m.messages)-i-1)
						break
					}
				}
			}
			if msg.older {
				m.scroll = max(0, len(m.messages)-max(1, m.height-5))
			}
			m.status = fmt.Sprintf("%d messages", len(m.messages))
			if len(msg.messages) == 0 && msg.older {
				m.status = "No older messages"
			}
		}
	case searchMsg:
		if m.active == 0 || (msg.id != 0 && msg.id != m.active) || (msg.epoch != 0 && msg.epoch != m.epoch) || (msg.query != "" && msg.query != m.query) || (msg.older && m.mode != "results") {
			break
		}
		if len(msg.messages) > 0 && msg.messages[0].ChatID != m.active {
			break
		}
		m.busy = false
		if msg.err != nil {
			m.status = "Search failed: " + msg.err.Error()
		} else {
			if msg.older {
				m.results = merge(m.results, msg.messages)
			} else {
				m.results = msg.messages
				m.resultIndex = 0
			}
			m.mode = "results"
			m.status = fmt.Sprintf("%d results", len(m.results))
			if msg.older && len(msg.messages) == 0 {
				m.status = "No older results"
			}
		}
	case sendMsg:
		if !m.sending || (msg.id != 0 && msg.id != m.active) || (msg.epoch != 0 && msg.epoch != m.epoch) {
			break
		}
		m.sending = false
		if msg.err != nil {
			m.status = "Send may have failed; inspect chat before retry: " + msg.err.Error()
			m.input.SetValue(msg.text)
		} else {
			m.status = "Sent"
			return m, m.loadHistory(m.active, 0, false)
		}
	case readMsg:
		if msg.id != 0 && (msg.id != m.active || (msg.epoch != 0 && msg.epoch != m.epoch)) {
			break
		}
		if msg.err != nil {
			m.status = "Read receipt failed: " + msg.err.Error()
		}
	case updateMsg:
		if !msg.ok {
			return m, nil
		}
		if msg.message.ChatID == m.active {
			m.messages = merge(m.messages, []app.Message{msg.message})
		}
		for i := range m.chats {
			if m.chats[i].ID == msg.message.ChatID && m.active != msg.message.ChatID && !msg.message.Out {
				if m.seenUnread == nil {
					m.seenUnread = make(map[int64]map[int]struct{})
				}
				seen := m.seenUnread[msg.message.ChatID]
				if seen == nil {
					seen = make(map[int]struct{})
					m.seenUnread[msg.message.ChatID] = seen
				}
				if _, duplicate := seen[msg.message.ID]; !duplicate {
					if len(seen) >= 4096 {
						clear(seen)
					}
					seen[msg.message.ID] = struct{}{}
					m.chats[i].Unread++
				}
			}
		}
		return m, m.listen()
	case stateMsg:
		if !msg.ok {
			return m, nil
		}
		m.status = "Connection: " + msg.state
		if msg.state == "ready" {
			if m.seenReady {
				commands := []tea.Cmd{m.loadChats()}
				if m.active != 0 {
					commands = append(commands, m.loadHistory(m.active, 0, false))
				}
				commands = append(commands, m.listenState())
				return m, tea.Batch(commands...)
			}
			m.seenReady = true
		}
		return m, m.listenState()
	case tea.KeyPressMsg:
		key := msg.String()
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		if m.mode == "compose" || m.mode == "search" {
			if key == "esc" {
				m.mode = "chat"
				m.input.Blur()
				return m, nil
			}
			if key == "enter" {
				value := strings.TrimSpace(m.input.Value())
				if value == "" {
					return m, nil
				}
				if m.mode == "compose" {
					if m.sending {
						return m, nil
					}
					m.sending = true
					m.status = "Sending..."
					m.input.Reset()
					return m, m.send(m.active, value)
				}
				m.query = value
				m.input.Reset()
				m.input.Blur()
				m.mode = "chat"
				m.busy = true
				m.status = "Searching..."
				return m, m.search(m.active, value, 0, false)
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		switch m.mode {
		case "list":
			switch key {
			case "q":
				return m, tea.Quit
			case "j", "down":
				if m.selected < len(m.chats)-1 {
					m.selected++
				}
			case "k", "up":
				if m.selected > 0 {
					m.selected--
				}
			case "r":
				m.busy = true
				m.status = "Refreshing..."
				return m, m.loadChats()
			case "enter":
				if len(m.chats) > 0 {
					c := m.chats[m.selected]
					m.active = c.ID
					m.epoch++
					m.sending = false
					m.mode = "chat"
					m.messages = nil
					m.scroll = 0
					m.busy = true
					m.status = "Loading history..."
					m.chats[m.selected].Unread = 0
					return m, tea.Batch(m.loadHistory(c.ID, 0, false), m.markRead(c.ID, c.TopID))
				}
			}
		case "chat":
			switch key {
			case "esc", "q":
				m.mode = "list"
				m.active = 0
				m.epoch++
				m.sending = false
			case "u":
				if !m.busy {
					before := 0
					if len(m.messages) > 0 {
						before = m.messages[0].ID
					}
					m.busy = true
					m.status = "Loading older..."
					return m, m.loadHistory(m.active, before, true)
				}
			case "r":
				m.busy = true
				return m, m.loadHistory(m.active, 0, false)
			case "pgup", "ctrl+u":
				m.scroll = min(max(0, len(m.messages)-1), m.scroll+max(1, m.height/2))
			case "pgdown", "ctrl+d":
				m.scroll = max(0, m.scroll-max(1, m.height/2))
			case "i":
				m.mode = "compose"
				return m, m.input.Focus()
			case "/":
				m.mode = "search"
				return m, m.input.Focus()
			}
		case "results":
			switch key {
			case "u":
				if !m.busy && len(m.results) > 0 {
					m.busy = true
					m.status = "Loading older results..."
					return m, m.search(m.active, m.query, m.results[0].ID, true)
				}
			case "esc", "q":
				m.mode = "chat"
			case "j", "down":
				if m.resultIndex < len(m.results)-1 {
					m.resultIndex++
				}
			case "k", "up":
				if m.resultIndex > 0 {
					m.resultIndex--
				}
			case "enter":
				if len(m.results) > 0 {
					target := m.results[m.resultIndex].ID
					m.messages = merge(m.messages, []app.Message{m.results[m.resultIndex]})
					for i, v := range m.messages {
						if v.ID == m.results[m.resultIndex].ID {
							m.scroll = max(0, len(m.messages)-i-1)
							break
						}
					}
					m.mode = "chat"
					m.status = fmt.Sprintf("Loading context for #%d...", target)
					return m, m.loadContext(m.active, target)
				}
			}
		}
	}
	return m, nil
}

func merge(existing, incoming []app.Message) []app.Message {
	byID := make(map[int]app.Message, len(existing)+len(incoming))
	for _, v := range existing {
		byID[v.ID] = v
	}
	for _, v := range incoming {
		byID[v.ID] = v
	}
	out := make([]app.Message, 0, len(byID))
	for _, v := range byID {
		out = append(out, v)
	}
	sortMessages(out)
	return out
}

func sortMessages(v []app.Message) { sort.Slice(v, func(i, j int) bool { return v[i].ID < v[j].ID }) }

func (m Model) View() tea.View {
	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")).Render("Telegram TUI")
	lines := []string{header}
	limit := max(1, m.height-5)
	switch m.mode {
	case "list":
		start := max(0, m.selected-limit+1)
		for i := start; i < len(m.chats) && i < start+limit; i++ {
			c := m.chats[i]
			prefix := "  "
			if i == m.selected {
				prefix = "> "
			}
			lines = append(lines, fmt.Sprintf("%s%s [%s] (%d)", prefix, singleLine(c.Title), c.Kind, c.Unread))
		}
		if len(m.chats) == 0 {
			lines = append(lines, "No chats loaded")
		}
		lines = append(lines, "j/k move  enter open  r refresh  q quit")
	case "results":
		lines = append(lines, "Search results")
		start := max(0, m.resultIndex-limit+1)
		for i := start; i < len(m.results) && i < start+limit; i++ {
			v := m.results[i]
			p := "  "
			if i == m.resultIndex {
				p = "> "
			}
			lines = append(lines, p+singleLine(v.Text))
		}
		lines = append(lines, "j/k move  u older  enter select  esc back")
	default:
		lines = append(lines, m.chatTitle())
		start := max(0, len(m.messages)-limit-m.scroll)
		end := min(len(m.messages), start+limit)
		for _, v := range m.messages[start:end] {
			who := "←"
			if v.Out {
				who = "→"
			}
			lines = append(lines, fmt.Sprintf("%s %s %s", time.Unix(int64(v.Date), 0).Format("15:04"), who, singleLine(v.Text)))
		}
		if m.mode == "compose" || m.mode == "search" {
			lines = append(lines, m.input.View())
		} else {
			lines = append(lines, "i compose  / search  u older  PgUp/PgDn scroll  r refresh  esc chats")
		}
	}
	lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(m.status))
	v := tea.NewView(strings.Join(lines, "\n"))
	v.AltScreen = true
	return v
}

func (m Model) chatTitle() string {
	for _, c := range m.chats {
		if c.ID == m.active {
			return singleLine(c.Title)
		}
	}
	return "Chat"
}
func singleLine(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}
