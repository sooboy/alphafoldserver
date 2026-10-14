// Development/build helper only. The deployed afplatform binary does not need it.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func goExe() string {
	if v := os.Getenv("AF_GO"); v != "" {
		return v
	}
	return "go"
}
func command(env []string, name string, args ...string) error {
	fmt.Printf("+ %s %s\n", name, strings.Join(args, " "))
	c := exec.Command(name, args...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Env = os.Environ()
	c.Env = append(c.Env, env...)
	return c.Run()
}
func npm(args ...string) error {
	args = append([]string{"--prefix", "frontend"}, args...)
	if runtime.GOOS != "windows" {
		return command(nil, "npm", args...)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		return err
	}
	cli := os.Getenv("AF_NPM_CLI")
	if cli == "" {
		cli = filepath.Join(filepath.Dir(node), "node_modules", "npm", "bin", "npm-cli.js")
	}
	if _, err = os.Stat(cli); err != nil {
		return fmt.Errorf("npm CLI not found; set AF_NPM_CLI to npm-cli.js: %w", err)
	}
	return command(nil, node, append([]string{cli}, args...)...)
}
func projectRoot() (string, error) {
	root, err := os.Getwd()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil || !strings.Contains(string(b), "module github.com/sooboy/alphafoldserver") {
		return "", errors.New("run from the alphafoldserver repository root")
	}
	return root, nil
}
func outputDir(root, rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) {
		return "", errors.New("output directory must be relative to repository")
	}
	p := filepath.Join(root, rel)
	r, err := filepath.Rel(root, p)
	if err != nil || r == "." || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", errors.New("output directory escapes repository")
	}
	first := strings.Split(filepath.ToSlash(r), "/")[0]
	if first != "build" && first != "dist" {
		return "", errors.New("output must be under build/ or dist/; data and source paths are protected")
	}
	// Refuse symlink/junction ancestors before writing outputs.
	for cur := p; cur != root; cur = filepath.Dir(cur) {
		info, e := os.Lstat(cur)
		if e == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("output symlink/junction is not allowed")
		}
		if e != nil && !os.IsNotExist(e) {
			return "", e
		}
	}
	return p, nil
}
func executableName(goos string) string {
	if goos == "windows" {
		return "afplatform.exe"
	}
	return "afplatform"
}
func build(goos, arch, dest string) error {
	if goos != "windows" && goos != "linux" && goos != "darwin" {
		return errors.New("unsupported OS")
	}
	if arch != "amd64" && arch != "arm64" {
		return errors.New("unsupported architecture")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	return command([]string{"GOOS=" + goos, "GOARCH=" + arch, "CGO_ENABLED=0"}, goExe(), "build", "-trimpath", "-buildvcs=false", "-o", dest, "./cmd/afplatform")
}
func copyFile(src, dest string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dest, b, 0644)
}
func release(dir string) error {
	type artifact struct {
		File   string `json:"file"`
		OS     string `json:"os"`
		Arch   string `json:"arch"`
		Bytes  int    `json:"bytes"`
		SHA256 string `json:"sha256"`
	}
	rows := []artifact{}
	for _, target := range [][2]string{{"windows", "amd64"}, {"linux", "amd64"}, {"linux", "arm64"}} {
		name := "afplatform-" + target[0] + "-" + target[1]
		if target[0] == "windows" {
			name += ".exe"
		}
		p := filepath.Join(dir, name)
		if err := build(target[0], target[1], p); err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		rows = append(rows, artifact{name, target[0], target[1], len(b), hex.EncodeToString(sum[:])})
	}
	if err := copyFile("docs/THIRD_PARTY_NOTICES.txt", filepath.Join(dir, "THIRD_PARTY_NOTICES.txt")); err != nil {
		return err
	}
	b, err := json.MarshalIndent(map[string]any{"artifacts": rows, "linux_runtime_tested": false}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "build-manifest.json"), append(b, '\n'), 0644)
}

var cachePaths = []string{"frontend/dist", "frontend/test-results", "frontend/playwright-report"}

func cacheTargets(root string) ([]string, error) {
	targets := []string{}
	for _, rel := range cachePaths {
		p := filepath.Join(root, filepath.FromSlash(rel))
		r, err := filepath.Rel(root, p)
		if err != nil || r == "." || strings.HasPrefix(r, "..") {
			return nil, errors.New("unsafe cleanup path")
		}
		// Check all existing ancestors before walking; never follow directory links.
		for cur := p; cur != root; cur = filepath.Dir(cur) {
			info, e := os.Lstat(cur)
			if e == nil && info.Mode()&os.ModeSymlink != 0 {
				return nil, errors.New("cleanup symlink/junction refused: " + cur)
			}
			if e != nil && !os.IsNotExist(e) {
				return nil, e
			}
		}
		if _, err = os.Lstat(p); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		err = filepath.WalkDir(p, func(path string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.Type()&os.ModeSymlink != 0 {
				return errors.New("cleanup link refused: " + path)
			}
			switch strings.ToLower(d.Name()) {
			case "state.json", "setup-token.txt", "initial-admin-password.txt", "profiles", "downloads", ".git":
				return errors.New("protected data detected; cleanup aborted: " + path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		targets = append(targets, p)
	}
	return targets, nil
}
func clean(root string) error {
	targets, err := cacheTargets(root)
	if err != nil {
		return err
	}
	for _, p := range targets {
		fmt.Println("remove generated cache:", p)
		if err = os.RemoveAll(p); err != nil {
			return err
		}
	}
	fmt.Println("Preserved binaries, private data, node_modules, embedded assets, archives and verification records.")
	return nil
}
func status(base string) error {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("URL must be an http(s) origin without path, credentials or query")
	}
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("unexpected redirect") }}
	r, err := client.Get(base + "/api/v1/setup")
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return fmt.Errorf("service HTTP %d", r.StatusCode)
	}
	var state struct {
		Required *bool `json:"required"`
	}
	if err = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&state); err != nil {
		return err
	}
	if state.Required == nil {
		return errors.New("not a platform setup response")
	}
	fmt.Printf("HTTP 200; service reachable; initialized=%t (not a Google/quota check)\n", !*state.Required)
	return nil
}
func run(args []string) error {
	if len(args) == 0 {
		args = []string{"help"}
	}
	if args[0] == "help" {
		fmt.Print(`FoldSpace development and operations
  make deps           Download Go modules and install locked frontend dependencies
  make build          Native Go-only binary -> build/afplatform[.exe]
  make frontend       Rebuild and embed the frontend (npm dependencies required)
  make release        Windows amd64 + Linux amd64/arm64 -> dist/ + manifest/licenses
  make check          Go vet and unit/API/cleanup tests
  make test-api       Execute the documented HTTP requests against isolated fixtures
  make test-race      Go race tests (a C toolchain is required)
  make ui-test        Build an isolated server and run Playwright (installed browser required)
  make verify         Full checks, UI build/tests, API probe and release build
  make run / serve    Build+foreground run / run the existing native build
  make status         Check platform HTTP availability, not account readiness
  make clean          Remove generated frontend caches only; NEVER private data/binaries
  make service-start|service-stop|service-restart|service-status|service-logs
                      Linux/systemd only; installation and permissions are manual
Variables: TARGET_OS TARGET_ARCH OUT RELEASE_DIR DATA LISTEN PUBLIC_URL BROWSER MAX_BROWSERS SERVICE
Windows: use mingw32-make instead of make if that is your installed command.
`)
		return nil
	}
	root, err := projectRoot()
	if err != nil {
		return err
	}
	switch args[0] {
	case "npm":
		return npm(args[1:]...)
	case "frontend":
		return npm("run", "build")
	case "clean":
		return clean(root)
	case "verify":
		return command(nil, "node", "scripts/verify.mjs")
	case "ui-test":
		file := filepath.Join("private", "platform-v2", "modified")
		if runtime.GOOS == "windows" {
			file += ".exe"
		}
		if err = build(runtime.GOOS, runtime.GOARCH, file); err != nil {
			return err
		}
		return npm("test")
	case "build", "release":
		f := flag.NewFlagSet(args[0], flag.ContinueOnError)
		goos := f.String("os", runtime.GOOS, "target OS")
		arch := f.String("arch", runtime.GOARCH, "target architecture")
		def := "build"
		if args[0] == "release" {
			def = "dist"
		}
		out := f.String("out", def, "output directory under build/ or dist/")
		if err = f.Parse(args[1:]); err != nil {
			return err
		}
		if f.NArg() != 0 {
			return errors.New("unexpected build arguments")
		}
		dir, err := outputDir(root, *out)
		if err != nil {
			return err
		}
		if args[0] == "release" {
			return release(dir)
		}
		if err = build(*goos, *arch, filepath.Join(dir, executableName(*goos))); err != nil {
			return err
		}
		return copyFile("docs/THIRD_PARTY_NOTICES.txt", filepath.Join(dir, "THIRD_PARTY_NOTICES.txt"))
	case "serve":
		f := flag.NewFlagSet("serve", flag.ContinueOnError)
		out := f.String("out", "build", "binary directory")
		data := f.String("data", "private/go-platform", "private data")
		listen := f.String("listen", "127.0.0.1:8788", "listener")
		origin := f.String("public-url", "http://127.0.0.1:8788", "exact public origin")
		browser := f.String("browser", "", "browser executable")
		max := f.Int("max-active-browsers", 3, "active browser cap")
		if err = f.Parse(args[1:]); err != nil {
			return err
		}
		if f.NArg() != 0 {
			return errors.New("unexpected serve arguments")
		}
		dir, err := outputDir(root, *out)
		if err != nil {
			return err
		}
		return command(nil, filepath.Join(dir, executableName(runtime.GOOS)), "serve", "-data", *data, "-listen", *listen, "-public-url", *origin, "-browser", *browser, "-max-active-browsers", fmt.Sprint(*max))
	case "status":
		f := flag.NewFlagSet("status", flag.ContinueOnError)
		base := f.String("url", "http://127.0.0.1:8788", "public origin")
		if err = f.Parse(args[1:]); err != nil {
			return err
		}
		if f.NArg() != 0 {
			return errors.New("unexpected status arguments")
		}
		return status(*base)
	default:
		return errors.New("unknown action: " + args[0])
	}
}
