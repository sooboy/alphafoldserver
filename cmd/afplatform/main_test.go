package main

import (
	"github.com/sooboy/alphafoldserver/internal/platform"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitAndImportOnlyMetadata(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "data")
	if err := run([]string{"init", "-data", data}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(data, "initial-admin-password.txt"))
	if err != nil || len(strings.TrimSpace(string(b))) != 64 {
		t.Fatal("generated private password", err)
	}
	if err = run([]string{"init", "-data", data}); err == nil || err.Error() != "ALREADY_INITIALIZED" {
		t.Fatal(err)
	}
	legacy := filepath.Join(dir, "legacy.json")
	raw := `{"accounts":[{"id":"a","label":"fixture","enabled":true,"identity_hash":"MUST_NOT_IMPORT","profile":"MUST_NOT_OPEN","cookie":"MUST_NOT_COPY"}],"jobs":{"other-owner":"MUST_NOT_IMPORT"}}`
	if err = os.WriteFile(legacy, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	if err = run([]string{"import-node-config", "-data", data, "-input", legacy}); err != nil {
		t.Fatal(err)
	}
	s, err := platform.OpenStore(data)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	st := s.Snapshot()
	if len(st.Accounts) != 1 || st.Accounts["a"].Status != "needs_login" || st.Accounts["a"].Identity != "" || len(st.Jobs) != 0 {
		t.Fatal("unsafe import")
	}
}
func TestUnsafeListenersRejected(t *testing.T) {
	cases := [][]string{{"serve", "-listen", "0.0.0.0:8788", "-public-url", "http://fold.example.org"}, {"serve", "-listen", "0.0.0.0:8788", "-public-url", "https://fold.example.org"}, {"serve", "-max-active-browsers", "0"}, {"serve", "-max-active-browsers", "21"}}
	for _, args := range cases {
		if err := run(args); err == nil {
			t.Fatal("unsafe config accepted", args)
		}
	}
}

func TestFreshImportDirectsToWebSetup(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "legacy.json")
	if err := os.WriteFile(legacy, []byte(`{"accounts":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	err := run([]string{"import-node-config", "-data", filepath.Join(dir, "data"), "-input", legacy})
	if err == nil || !strings.Contains(err.Error(), "WEB_SETUP_REQUIRED") || strings.Contains(err.Error(), "RUN_INIT_FIRST") {
		t.Fatal("fresh import should direct to web setup, not require init:", err)
	}
}

func TestHelpSucceedsWithoutSideEffects(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	for _, args := range [][]string{{"-h"}, {"--help"}, {"help"}, {"help", "init"}, {"help", "serve"}, {"help", "import-node-config"}, {"init", "-h"}, {"serve", "--help"}, {"import-node-config", "-h"}} {
		if err := run(args); err != nil {
			t.Errorf("help %v failed: %v", args, err)
		}
	}
	for _, args := range [][]string{{"not-a-command"}, {"serve", "--definitely-unknown"}, {"help", "not-a-command"}, {"help", "serve", "extra"}} {
		if err := run(args); err == nil {
			t.Errorf("invalid arguments accepted: %v", args)
		}
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatalf("help created files: %v, %v", files, err)
	}
}
