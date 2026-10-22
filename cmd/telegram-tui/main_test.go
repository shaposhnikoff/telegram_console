package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gotd/td/tg"
)

func TestBackupSessionPreservesEncryptedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.enc")
	original := []byte("encrypted session bytes")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	backup, err := backupSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("original session still exists: %v", err)
	}
	got, err := os.ReadFile(backup)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatalf("backup differs: %v", err)
	}
	info, err := os.Stat(backup)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("backup permissions: %v, %v", info, err)
	}
}

func TestCodePromptShowsDeliveryChannel(t *testing.T) {
	tests := []struct {
		name      string
		typeValue tg.AuthSentCodeTypeClass
		want      string
	}{
		{"app", &tg.AuthSentCodeTypeApp{}, "Telegram sign-in code from your Telegram app: "},
		{"sms", &tg.AuthSentCodeTypeSMS{}, "Telegram sign-in code from SMS: "},
		{"call", &tg.AuthSentCodeTypeCall{}, "Telegram sign-in code from phone call: "},
		{"email", &tg.AuthSentCodeTypeEmailCode{}, "Telegram sign-in code from email: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := codePrompt(&tg.AuthSentCode{Type: tt.typeValue}); got != tt.want {
				t.Fatalf("prompt = %q, want %q", got, tt.want)
			}
		})
	}
}
