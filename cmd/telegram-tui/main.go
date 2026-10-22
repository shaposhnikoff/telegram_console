package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	tea "charm.land/bubbletea/v2"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/telegram/updates/hook"
	"github.com/gotd/td/tg"
	"github.com/shaposhnikoff/telegram_console/internal/config"
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
	flags := flag.NewFlagSet("telegram-tui", flag.ContinueOnError)
	logout := flags.Bool("logout", false, "revoke the Telegram session")
	reset := flags.Bool("reset-session", false, "back up the local session and sign in again")
	configPath := flags.String("config", "", "path to config.json")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || (*logout && *reset) {
		return errors.New("usage: telegram-tui [--config path] [--logout | --reset-session]")
	}
	if *configPath == "" {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		*configPath = filepath.Join(filepath.Dir(exe), "config.json")
	}
	settings, err := config.Load(*configPath)
	if err != nil {
		return err
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
	if *reset && sessionErr == nil {
		fmt.Fprint(os.Stderr, "Back up the existing session and sign in again? Type RESET: ")
		answer, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		if strings.TrimSpace(answer) != "RESET" {
			return errors.New("session reset cancelled")
		}
		backupPath, err := backupSession(path)
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "Previous encrypted session saved at", backupPath)
		sessionErr = os.ErrNotExist
	}
	if *logout {
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
	if errors.Is(sessionErr, os.ErrNotExist) && !*logout {
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
		if errors.Is(err, session.ErrPassphrase) {
			return fmt.Errorf("%w; enter the local passphrase chosen on first run, or use --reset-session to back up this file and sign in again: %s", err, path)
		}
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
	client := telegram.NewClient(settings.APIID, settings.APIHash, telegram.Options{
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
		if *logout {
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
		if err := client.Auth().IfNecessary(ctx, auth.NewFlow(prompter{reader: reader, phone: settings.Phone}, auth.SendCodeOptions{})); err != nil {
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
	if *logout {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		fmt.Fprintln(os.Stderr, "Logged out")
	}
	return nil
}

func backupSession(path string) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(path), "session.enc.backup-*")
	if err != nil {
		return "", err
	}
	backupPath := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(backupPath)
		return "", err
	}
	if err := os.Rename(path, backupPath); err != nil {
		_ = os.Remove(backupPath)
		return "", err
	}
	return backupPath, nil
}

type prompter struct {
	reader *bufio.Reader
	phone  string
}

func (p prompter) Phone(context.Context) (string, error) {
	if p.phone != "" {
		return p.phone, nil
	}
	return p.line("Phone number with country code: ")
}
func (p prompter) Code(_ context.Context, sent *tg.AuthSentCode) (string, error) {
	b, err := secret(codePrompt(sent))
	defer func() {
		for i := range b {
			b[i] = 0
		}
	}()
	return strings.TrimSpace(string(b)), err
}

func codePrompt(sent *tg.AuthSentCode) string {
	if sent == nil {
		return "Telegram sign-in code: "
	}
	switch sent.Type.(type) {
	case *tg.AuthSentCodeTypeApp:
		return "Telegram sign-in code from your Telegram app: "
	case *tg.AuthSentCodeTypeSMS, *tg.AuthSentCodeTypeFirebaseSMS, *tg.AuthSentCodeTypeFragmentSMS,
		*tg.AuthSentCodeTypeSMSWord, *tg.AuthSentCodeTypeSMSPhrase:
		return "Telegram sign-in code from SMS: "
	case *tg.AuthSentCodeTypeCall, *tg.AuthSentCodeTypeFlashCall, *tg.AuthSentCodeTypeMissedCall:
		return "Telegram sign-in code from phone call: "
	case *tg.AuthSentCodeTypeEmailCode:
		return "Telegram sign-in code from email: "
	default:
		return "Telegram sign-in code: "
	}
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
