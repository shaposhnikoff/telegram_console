package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	tea "charm.land/bubbletea/v2"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/telegram/updates/hook"
	"github.com/gotd/td/tg"
	"github.com/shaposhnikoff/telegram_console/internal/session"
	backend "github.com/shaposhnikoff/telegram_console/internal/telegram"
	"github.com/shaposhnikoff/telegram_console/internal/ui"
	"golang.org/x/term"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "telegram-tui:", err)
		os.Exit(1)
	}
}

func run() error {
	logout := len(os.Args) == 2 && os.Args[1] == "--logout"
	if len(os.Args) > 1 && !logout {
		return errors.New("usage: telegram-tui [--logout]")
	}
	appID, err := strconv.Atoi(os.Getenv("TELEGRAM_API_ID"))
	if err != nil || appID <= 0 {
		return errors.New("set TELEGRAM_API_ID to your numeric Telegram app ID")
	}
	appHash := os.Getenv("TELEGRAM_API_HASH")
	if appHash == "" {
		return errors.New("set TELEGRAM_API_HASH")
	}
	dataDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dataDir, "telegram-tui", "session.enc")
	_, sessionErr := os.Stat(path)
	if sessionErr != nil && !errors.Is(sessionErr, os.ErrNotExist) {
		return sessionErr
	}
	reader := bufio.NewReader(os.Stdin)
	if logout {
		fmt.Fprint(os.Stderr, "Log out of Telegram and remove the local session? Type yes: ")
		answer, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		if strings.TrimSpace(answer) != "yes" {
			return errors.New("logout cancelled")
		}
	}
	pass, err := secret("Session passphrase: ")
	if err != nil {
		return err
	}
	if errors.Is(sessionErr, os.ErrNotExist) && !logout {
		confirmation, err := secret("Confirm new session passphrase: ")
		if err != nil {
			return err
		}
		matches := bytes.Equal(pass, confirmation)
		for i := range confirmation {
			confirmation[i] = 0
		}
		if !matches {
			for i := range pass {
				pass[i] = 0
			}
			return errors.New("session passphrases do not match")
		}
	}
	store := session.New(path, pass)
	for i := range pass {
		pass[i] = 0
	}
	defer store.Close()
	plain, err := store.LoadSession(context.Background())
	if err != nil {
		return err
	}
	for i := range plain {
		plain[i] = 0
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var b *backend.Backend
	gaps := updates.New(updates.Config{
		Handler: telegram.UpdateHandlerFunc(func(ctx context.Context, update tg.UpdatesClass) error {
			if b == nil {
				return nil
			}
			return b.HandleUpdates(ctx, update)
		}),
		OnTooLong: func() {
			if b != nil {
				b.SetStatus("update gap too long; refresh chats")
			}
		},
		OnChannelTooLong: func(int64) {
			if b != nil {
				b.SetStatus("channel update gap; refresh chat")
			}
		},
	})
	client := telegram.NewClient(appID, appHash, telegram.Options{
		SessionStorage: store,
		Middlewares:    []telegram.Middleware{hook.AffectedHook(gaps)},
		OnConnectionState: func(state telegram.ConnectionState) {
			if b != nil {
				b.SetConnectionState(state)
			}
		},
		UpdateHandler: gaps,
	})
	b = backend.New(client)
	err = client.Run(ctx, func(ctx context.Context) error {
		if logout {
			status, err := client.Auth().Status(ctx)
			if err != nil {
				return err
			}
			if status.Authorized {
				if _, err := client.API().AuthLogOut(ctx); err != nil {
					return err
				}
			}
			return nil
		}
		if err := client.Auth().IfNecessary(ctx, auth.NewFlow(prompter{reader}, auth.SendCodeOptions{})); err != nil {
			return fmt.Errorf("authentication: %w", err)
		}
		self, err := client.Self(ctx)
		if err != nil {
			return fmt.Errorf("current user: %w", err)
		}
		go func() {
			if err := gaps.Run(ctx, client.API(), self.ID, updates.AuthOptions{}); err != nil && ctx.Err() == nil {
				b.SetStatus("update recovery failed; refresh chats")
			}
		}()
		_, err = tea.NewProgram(ui.New(ctx, b)).Run()
		return err
	})
	if err != nil {
		return err
	}
	if logout {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		fmt.Fprintln(os.Stderr, "Logged out")
	}
	return nil
}

type prompter struct{ reader *bufio.Reader }

func (p prompter) Phone(context.Context) (string, error) {
	return p.line("Phone number with country code: ")
}
func (p prompter) Code(context.Context, *tg.AuthSentCode) (string, error) {
	b, err := secret("Telegram sign-in code: ")
	defer func() {
		for i := range b {
			b[i] = 0
		}
	}()
	return string(b), err
}
func (p prompter) Password(context.Context) (string, error) {
	b, err := secret("Telegram 2FA password: ")
	defer func() {
		for i := range b {
			b[i] = 0
		}
	}()
	return string(b), err
}
func (p prompter) AcceptTermsOfService(context.Context, tg.HelpTermsOfService) error {
	return errors.New("Telegram terms of service require acceptance in another client")
}
func (p prompter) SignUp(context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("create the account in an official Telegram client first")
}
func (p prompter) line(label string) (string, error) {
	fmt.Fprint(os.Stderr, label)
	v, err := p.reader.ReadString('\n')
	return strings.TrimSpace(v), err
}

func secret(label string) ([]byte, error) {
	fmt.Fprint(os.Stderr, label)
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return nil, errors.New("interactive terminal required")
	}
	v, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return v, err
}
