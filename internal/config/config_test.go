package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"api_id":123,"api_hash":"hash","phone":" +123 "}`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || got.APIID != 123 || got.APIHash != "hash" || got.Phone != "+123" {
		t.Fatalf("Load() = %+v, %v", got, err)
	}
}

func TestLoadRejectsInvalidOrExposedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"api_id":0,"api_hash":""}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("accepted missing credentials")
	}
	if err := os.WriteFile(path, []byte(`{"api_id":123,"api_hash":"hash"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("accepted public configuration: %v", err)
	}
}
