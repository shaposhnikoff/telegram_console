package session

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	version        = byte(1)
	saltSize       = 16
	maxSessionSize = 4 << 20
)

var ErrPassphrase = errors.New("invalid passphrase or damaged session")

// Storage persists gotd's opaque session in an encrypted, private file.
type Storage struct {
	mu         sync.Mutex
	path       string
	passphrase []byte
}

func New(path string, passphrase []byte) *Storage {
	return &Storage{path: path, passphrase: append([]byte(nil), passphrase...)}
}

func (s *Storage) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.passphrase {
		s.passphrase[i] = 0
	}
}

func (s *Storage) LoadSession(_ context.Context) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) > maxSessionSize || len(data) < 1+saltSize+chacha20poly1305.NonceSizeX+16 || data[0] != version {
		return nil, ErrPassphrase
	}
	salt := data[1 : 1+saltSize]
	nonce := data[1+saltSize : 1+saltSize+chacha20poly1305.NonceSizeX]
	key := argon2.IDKey(s.passphrase, salt, 3, 64*1024, 4, chacha20poly1305.KeySize)
	defer zero(key)
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	plain, err := aead.Open(nil, nonce, data[1+saltSize+chacha20poly1305.NonceSizeX:], data[:1])
	if err != nil {
		return nil, ErrPassphrase
	}
	return plain, nil
}

func (s *Storage) StoreSession(_ context.Context, plain []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(plain) > maxSessionSize-128 {
		return fmt.Errorf("session too large")
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	salt := make([]byte, saltSize)
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return err
	}
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	key := argon2.IDKey(s.passphrase, salt, 3, 64*1024, 4, chacha20poly1305.KeySize)
	defer zero(key)
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return err
	}
	data := append([]byte{version}, salt...)
	data = append(data, nonce...)
	data = aead.Seal(data, nonce, plain, []byte{version})
	f, err := os.CreateTemp(filepath.Dir(s.path), ".session-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := f.Chmod(0600); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), s.path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(s.path))
	if err == nil {
		defer dir.Close()
		_ = dir.Sync()
	}
	return nil
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
