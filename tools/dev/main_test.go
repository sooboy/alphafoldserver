package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func put(t *testing.T, root, p string) {
	t.Helper()
	p = filepath.Join(root, p)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestCleanOnlyGeneratedCaches(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"frontend/dist/app.js", "frontend/test-results/log.txt", "private/state.json", "private/profiles/test/cookie", "dist/running.exe", "build/running.exe", "internal/platform/assets/app.js", "frontend/node_modules/a/index.js", "transactions/VERIFICATION.txt"} {
		put(t, root, p)
	}
	if err := clean(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "frontend/dist")); !os.IsNotExist(err) {
		t.Fatal("cache remains")
	}
	for _, p := range []string{"private/state.json", "private/profiles/test/cookie", "dist/running.exe", "build/running.exe", "internal/platform/assets/app.js", "frontend/node_modules/a/index.js", "transactions/VERIFICATION.txt"} {
		if _, err := os.Stat(filepath.Join(root, p)); err != nil {
			t.Fatal("protected file removed", p)
		}
	}
}
func TestCleanPreflightProtectsAllTargets(t *testing.T) {
	root := t.TempDir()
	put(t, root, "frontend/dist/app.js")
	put(t, root, "frontend/test-results/state.json")
	if clean(root) == nil {
		t.Fatal("accepted protected data")
	}
	if _, err := os.Stat(filepath.Join(root, "frontend/dist/app.js")); err != nil {
		t.Fatal("partial cleanup")
	}
}
func TestCleanRefusesSymlinks(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	put(t, outside, "precious.txt")
	if err := os.MkdirAll(filepath.Join(root, "frontend"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "frontend/dist")); err != nil {
		t.Skip("symlinks not permitted by this OS user")
	}
	if clean(root) == nil {
		t.Fatal("followed link")
	}
	if _, err := os.Stat(filepath.Join(outside, "precious.txt")); err != nil {
		t.Fatal("outside file lost")
	}
}
func TestOutputPathGuards(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"", ".", "..", "build/../../outside", "private", "internal/platform/assets", filepath.Join(root, "dist")} {
		if _, err := outputDir(root, p); err == nil {
			t.Fatal("unsafe output accepted", p)
		}
	}
	for _, p := range []string{"build", "dist", "build/custom"} {
		if _, err := outputDir(root, p); err != nil {
			t.Fatal(err)
		}
	}
}
func TestStatusChecksResponse(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/setup" {
			t.Error(r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"required":false}`))
	}))
	defer s.Close()
	if err := status(s.URL); err != nil {
		t.Fatal(err)
	}
	if status(s.URL+"/unexpected") == nil {
		t.Fatal("accepted a path")
	}
}
