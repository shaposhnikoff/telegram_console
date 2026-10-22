package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Config holds the Telegram application credentials and an optional login phone.
type Config struct {
	APIID   int    `json:"api_id"`
	APIHash string `json:"api_hash"`
	Phone   string `json:"phone"`
}

// Load reads a private JSON configuration file.
func Load(path string) (Config, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, fmt.Errorf("configuration not found at %s; copy config.example.json next to the binary as config.json and fill it in", path)
		}
		return Config{}, fmt.Errorf("configuration: %w", err)
	}
	if !info.Mode().IsRegular() {
		return Config{}, errors.New("configuration must be a regular file")
	}
	if info.Mode().Perm()&0077 != 0 {
		return Config{}, fmt.Errorf("configuration %s is readable by other users; run chmod 600 on it", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("configuration: %w", err)
	}
	defer f.Close()
	var value Config
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}
	if decoder.Decode(new(any)) != io.EOF {
		return Config{}, errors.New("invalid configuration: expected one JSON object")
	}
	if value.APIID <= 0 || strings.TrimSpace(value.APIHash) == "" {
		return Config{}, errors.New("set api_id and api_hash in config.json")
	}
	value.Phone = strings.TrimSpace(value.Phone)
	return value, nil
}
