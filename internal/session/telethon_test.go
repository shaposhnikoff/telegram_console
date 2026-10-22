package session

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	gotdsession "github.com/gotd/td/session"
)

func TestReadTelethonAndSaveEncrypted(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "anon.session")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE sessions (dc_id INTEGER, server_address TEXT, port INTEGER, auth_key BLOB)"); err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{0x42}, 256)
	if _, err := db.Exec("INSERT INTO sessions VALUES (?, ?, ?, ?)", 2, "149.154.167.51", 443, key); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := ReadTelethon(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if data.DC != 2 || data.Addr != "149.154.167.51:443" || !bytes.Equal(data.AuthKey, key) {
		t.Fatal("imported connection data differs")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, before) {
		t.Fatalf("Telethon source changed: %v", err)
	}
	store := New(filepath.Join(t.TempDir(), "session.enc"), []byte("test passphrase"))
	defer store.Close()
	loader := gotdsession.Loader{Storage: store}
	if err := loader.Save(ctx, data); err != nil {
		t.Fatal(err)
	}
	got, err := loader.Load(ctx)
	if err != nil || !bytes.Equal(got.AuthKey, key) {
		t.Fatalf("encrypted imported session does not load: %v", err)
	}
}

func TestReadTelethonRejectsInvalidKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anon.session")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE sessions (dc_id INTEGER, server_address TEXT, port INTEGER, auth_key BLOB)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO sessions VALUES (?, ?, ?, ?)", 2, "149.154.167.51", 443, []byte("short")); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadTelethon(context.Background(), path); err == nil {
		t.Fatal("invalid auth key accepted")
	}
}
