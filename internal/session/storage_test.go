package session

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTripAndWrongPassphrase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "session.enc")
	ctx := context.Background()
	store := New(path, []byte("correct horse battery staple"))
	defer store.Close()
	if got, err := store.LoadSession(ctx); err != nil || got != nil {
		t.Fatalf("new session: %q, %v", got, err)
	}
	original := []byte("opaque MTProto session")
	if err := store.StoreSession(ctx, original); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("session mode: %v", info.Mode())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, original) {
		t.Fatal("plaintext session found on disk")
	}
	loaded, err := store.LoadSession(ctx)
	if err != nil || !bytes.Equal(loaded, original) {
		t.Fatalf("round trip: %q, %v", loaded, err)
	}
	wrong := New(path, []byte("wrong"))
	defer wrong.Close()
	if _, err := wrong.LoadSession(ctx); !errors.Is(err, ErrPassphrase) {
		t.Fatalf("wrong passphrase error: %v", err)
	}
	still, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(still, raw) {
		t.Fatalf("session changed after wrong password: %v", err)
	}
}

func TestStoreReplacesEncryptedSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.enc")
	store := New(path, []byte("passphrase"))
	defer store.Close()
	ctx := context.Background()
	for _, value := range []string{"first", "second"} {
		if err := store.StoreSession(ctx, []byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.LoadSession(ctx)
	if err != nil || string(got) != "second" {
		t.Fatalf("replacement: %q, %v", got, err)
	}
}
