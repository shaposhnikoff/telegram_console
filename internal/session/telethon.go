package session

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"

	"github.com/gotd/td/crypto"
	gotdsession "github.com/gotd/td/session"
	_ "modernc.org/sqlite"
)

// ReadTelethon reads an existing Telethon SQLite session without changing it.
func ReadTelethon(ctx context.Context, path string) (*gotdsession.Data, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("open Telethon session: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("Telethon session is not a regular file")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	uri := (&url.URL{Scheme: "file", Path: abs, RawQuery: "mode=ro&immutable=1"}).String()
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		return nil, fmt.Errorf("open Telethon database: %w", err)
	}
	defer db.Close()

	var dc, port int
	var address string
	var authKey []byte
	err = db.QueryRowContext(ctx, "SELECT dc_id, server_address, port, auth_key FROM sessions LIMIT 1").Scan(&dc, &address, &port, &authKey)
	if err != nil {
		return nil, fmt.Errorf("read Telethon session: %w", err)
	}
	if dc < 1 || dc > 255 || port < 1 || port > 65535 || net.ParseIP(address) == nil || len(authKey) != 256 {
		clear(authKey)
		return nil, fmt.Errorf("Telethon session contains invalid connection data")
	}
	var key crypto.Key
	copy(key[:], authKey)
	clear(authKey)
	id := key.ID()
	return &gotdsession.Data{
		DC:        dc,
		Addr:      net.JoinHostPort(address, fmt.Sprint(port)),
		AuthKey:   key[:],
		AuthKeyID: id[:],
	}, nil
}
