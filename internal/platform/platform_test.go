package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type mockBrowser struct {
	id      string
	closed  atomic.Bool
	eval    func(string, any) error
	onClose func()
}

func (b *mockBrowser) Eval(_ context.Context, expr string, out any) error {
	if b.closed.Load() {
		return fail("BROWSER_DISCONNECTED")
	}
	if b.eval != nil {
		return b.eval(expr, out)
	}
	return nil
}
func (b *mockBrowser) Close() error {
	if b.closed.CompareAndSwap(false, true) && b.onClose != nil {
		b.onClose()
	}
	return nil
}
func require(t *testing.T, ok bool, args ...any) {
	t.Helper()
	if !ok {
		t.Fatal(args...)
	}
}
func noerr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(t.TempDir())
	noerr(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

type remoteFixture struct {
	mu                sync.Mutex
	calls             map[string]int
	requests          []RPCRequest
	launches          int
	failAt, errorCode string
	identityOverride  string
}

func (f *remoteFixture) launcher(_ context.Context, id string, _ bool) (Browser, error) {
	f.mu.Lock()
	f.launches++
	f.mu.Unlock()
	return &mockBrowser{id: id, eval: func(expr string, out any) error {
		if b, ok := out.(*bool); ok {
			*b = true
			return nil
		}
		if strings.Contains(expr, "fold.zip") {
			raw := `{"base64":"UEsDBAAAAAAAAAAAAAAAAAAAAAAAAAA="}`
			return json.Unmarshal([]byte(raw), out)
		}
		const prefix = "(()=>{const __AF_POOL_REQUEST__="
		if !strings.HasPrefix(expr, prefix) {
			return fail("UNEXPECTED_EXPRESSION")
		}
		end := strings.Index(expr, ";return ")
		var q RPCRequest
		if err := json.Unmarshal([]byte(expr[len(prefix):end]), &q); err != nil {
			return err
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.calls == nil {
			f.calls = map[string]int{}
		}
		f.calls[q.RPC]++
		f.requests = append(f.requests, q)
		r := RPCResult{OK: true}
		if q.RPC == f.failAt {
			if f.errorCode == "TRANSPORT" {
				return fail("BROWSER_DISCONNECTED")
			}
			r.OK = false
			r.Error = f.errorCode
		} else {
			switch q.RPC {
			case "EU26qc":
				r.Identity = Hash(id)
				if f.identityOverride != "" {
					r.Identity = f.identityOverride
				}
			case "tjI49c":
				r.Payload = []any{[]any{30, true}}
			case "Gbje0":
				r.Payload = []any{1}
			case "kMnDgb":
				r.Payload = []any{[]any{"remote-" + id}, 5}
			case "bteK7":
				r.Payload = []any{[]any{[]any{"remote-" + id}, 1}}
			case "EXvXWc":
				r.Payload = []any{[]any{"remote-" + id}, 3, nil, nil, nil, nil, nil, []any{1, 2, 3, 4, 5}}
			default:
				return fail("UNEXPECTED_RPC")
			}
		}
		data, _ := json.Marshal(r)
		return json.Unmarshal(data, out)
	}}, nil
}
func (f *remoteFixture) count(rpc string) int { f.mu.Lock(); defer f.mu.Unlock(); return f.calls[rpc] }
func fixtureEngine(t *testing.T, n int) (*Engine, *remoteFixture) {
	t.Helper()
	s := newTestStore(t)
	f := &remoteFixture{}
	p := NewBrowserPool(3, time.Minute, f.launcher)
	t.Cleanup(p.Close)
	e := NewEngine(s, p)
	noerr(t, s.Update(func(s *State) error {
		s.Users["alice"] = User{ID: "alice", Role: "member"}
		s.Users["bob"] = User{ID: "bob", Role: "member"}
		s.Users["admin"] = User{ID: "admin", Role: "admin"}
		for i := 0; i < n; i++ {
			id := fmt.Sprintf("account-%03d", i)
			s.Accounts[id] = Account{ID: id, Enabled: true, Status: "ready"}
		}
		return nil
	}))
	return e, f
}
func group() Input {
	return Input{Name: "test-group", Sequences: []Sequence{{"TEACHINGS", 2}, {"ACDEFG", 1}}}
}

func TestBrowserPool50ProfilesBoundedAndPersistent(t *testing.T) {
	var active, peak, launched atomic.Int32
	var mu sync.Mutex
	busy := map[string]bool{}
	pool := NewBrowserPool(3, time.Minute, func(_ context.Context, id string, _ bool) (Browser, error) {
		n := active.Add(1)
		launched.Add(1)
		for n > peak.Load() {
			peak.CompareAndSwap(peak.Load(), n)
		}
		return &mockBrowser{id: id, onClose: func() { active.Add(-1) }}, nil
	})
	defer pool.Close()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, c := context.WithTimeout(context.Background(), 10*time.Second)
			defer c()
			id := fmt.Sprintf("account-%02d", i%50)
			l, err := pool.Acquire(ctx, id, false)
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			if busy[id] {
				t.Error("same account concurrently leased")
			}
			busy[id] = true
			mu.Unlock()
			time.Sleep(time.Millisecond)
			mu.Lock()
			busy[id] = false
			mu.Unlock()
			l.Release()
		}(i)
	}
	wg.Wait()
	require(t, peak.Load() <= 3 && peak.Load() > 0, peak.Load())
	pool.Close()
	require(t, active.Load() == 0, "browser leak")
	t.Logf("50 profiles / 100 operations: peak=%d limit=3 launched=%d active_after_close=%d", peak.Load(), launched.Load(), active.Load())
}
func TestBrowserPoolLifecycle(t *testing.T) {
	f := &remoteFixture{}
	p := NewBrowserPool(1, time.Millisecond, f.launcher)
	defer p.Close()
	ctx := context.Background()
	l, err := p.Acquire(ctx, "a", true)
	noerr(t, err)
	l.Release()
	_, err = p.Acquire(ctx, "a", false)
	require(t, err != nil && err.Error() == "LOGIN_WINDOW_OPEN", err)
	wait, c := context.WithTimeout(ctx, 20*time.Millisecond)
	defer c()
	_, err = p.Acquire(wait, "b", false)
	require(t, err != nil, "headed slot must count in limit")
	noerr(t, p.Stop("a"))
	l, err = p.Acquire(ctx, "b", false)
	noerr(t, err)
	require(t, p.Stop("b").Error() == "BROWSER_BUSY", "leased browser closed")
	l.Discard()
	require(t, p.Stats()["active"] == 0, "discard")
	l, err = p.Acquire(ctx, "b", false)
	noerr(t, err)
	l.Release()
	time.Sleep(5 * time.Millisecond)
	p.Sweep()
	require(t, p.Stats()["active"] == 0, "idle eviction")
}
func TestFailedLaunchReleasesSlot(t *testing.T) {
	p := NewBrowserPool(1, time.Second, func(context.Context, string, bool) (Browser, error) { return nil, fail("BROWSER_START_FAILED") })
	defer p.Close()
	_, err := p.Acquire(context.Background(), "a", false)
	require(t, err != nil && p.Stats()["active"] == 0, err)
	c, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = p.Acquire(c, "b", false)
	require(t, err != nil, "cancelled launch")
}

func TestTransactionBehavior(t *testing.T) {
	e, f := fixtureEngine(t, 50)
	j, err := e.Enqueue("alice", "same-request", group())
	noerr(t, err)
	e.process(context.Background(), j.ID)
	j, err = e.GetJob(User{ID: "alice"}, j.ID)
	noerr(t, err)
	require(t, j.Stage == "pending", j)
	require(t, f.count("tjI49c") == 1 && f.count("kMnDgb") == 1 && f.count("bteK7") == 1, f.calls)
	_, err = e.GetJob(User{ID: "bob"}, j.ID)
	require(t, err != nil && err.Error() == "NOT_FOUND", err)
	_, err = e.GetJob(User{ID: "admin", Role: "admin"}, j.ID)
	noerr(t, err)
	again, err := e.Enqueue("alice", "same-request", group())
	noerr(t, err)
	require(t, again.ID == j.ID, "idempotency")
	other, err := e.Enqueue("bob", "same-request", group())
	noerr(t, err)
	require(t, other.ID != j.ID, "owner scoped requests")
	for _, r := range f.requests {
		if r.RPC == "kMnDgb" {
			fold := arr(at(arr(r.Payload), 0))
			require(t, len(arr(at(fold, 1))) == 2, "group split")
		}
		if r.RPC == "bteK7" {
			job := arr(at(arr(r.Payload), 0))
			require(t, len(arr(at(job, 13))) == 1, "seed missing")
			seed := at(arr(at(job, 13)), 0).(float64)
			require(t, seed >= 0 && seed <= 4294967295, "seed bounds")
		}
	}
	t.Log(`TRANSACTION {"accounts":50,"limit":3,"quota_checks":1,"other_member":"NOT_FOUND","administrator":"visible","submission":"pending","group_entities":2}`)
}
func TestUnknownWriteIsNeverRetriedOrRotated(t *testing.T) {
	for _, rpc := range []string{"kMnDgb", "bteK7"} {
		t.Run(rpc, func(t *testing.T) {
			e, f := fixtureEngine(t, 2)
			f.failAt = rpc
			f.errorCode = "TRANSPORT"
			j, err := e.Enqueue("alice", "unknown", group())
			noerr(t, err)
			e.process(context.Background(), j.ID)
			j, _ = e.GetJob(User{ID: "alice"}, j.ID)
			require(t, j.Stage == "submission_unknown", j)
			e.process(context.Background(), j.ID)
			again, err := e.Enqueue("alice", "unknown", group())
			noerr(t, err)
			require(t, again.ID == j.ID && f.count("kMnDgb") == 1 && f.count(rpc) == 1, f.calls)
			require(t, e.claim() == "", "unknown was scheduled")
			require(t, e.Browsers.Stats()["active"] == 0, "broken browser not discarded")
		})
	}
}
func TestRateLimitPausesWholePool(t *testing.T) {
	e, f := fixtureEngine(t, 50)
	f.failAt = "tjI49c"
	f.errorCode = "RATE_LIMITED"
	j, err := e.Enqueue("alice", "rate", group())
	noerr(t, err)
	e.process(context.Background(), j.ID)
	require(t, f.count("tjI49c") == 1 && e.Store.Snapshot().CooldownUntil > stamp() && e.claim() == "", f.calls)
}
func TestIdentityMismatchNoWrite(t *testing.T) {
	e, f := fixtureEngine(t, 1)
	noerr(t, e.Store.Update(func(s *State) error {
		a := s.Accounts["account-000"]
		a.Identity = Hash("old")
		s.Accounts[a.ID] = a
		return nil
	}))
	f.identityOverride = Hash("new")
	j, err := e.Enqueue("alice", "identity", group())
	noerr(t, err)
	e.process(context.Background(), j.ID)
	require(t, !e.Store.Snapshot().Accounts["account-000"].Enabled && f.count("kMnDgb") == 0, "identity drift")
}
func TestPollAndDownloadStayOnOwnerAccount(t *testing.T) {
	e, f := fixtureEngine(t, 2)
	j, err := e.Enqueue("alice", "poll", group())
	noerr(t, err)
	e.process(context.Background(), j.ID)
	j, _ = e.GetJob(User{ID: "alice"}, j.ID)
	e.poll(context.Background(), j)
	j, _ = e.GetJob(User{ID: "alice"}, j.ID)
	require(t, j.Stage == "succeeded" && j.Models == 5, j)
	_, err = e.Download(context.Background(), User{ID: "bob"}, j.ID)
	require(t, err != nil && err.Error() == "NOT_FOUND", err)
	path, err := e.Download(context.Background(), User{ID: "alice"}, j.ID)
	noerr(t, err)
	b, err := os.ReadFile(path)
	noerr(t, err)
	require(t, strings.HasPrefix(string(b), "PK"), "zip")
	require(t, f.launches == 1, "wrong account")
}
func TestRestartRecoveryAndDataLock(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	noerr(t, err)
	_, err = OpenStore(dir)
	require(t, err != nil && err.Error() == "PLATFORM_DATA_LOCKED", err)
	noerr(t, s.Update(func(s *State) error {
		for _, stage := range []string{"preflight", "creating", "draft", "submitting", "pending"} {
			s.Jobs[stage] = Job{ID: stage, Stage: stage, AccountID: "a"}
		}
		return nil
	}))
	noerr(t, s.Close())
	s, err = OpenStore(dir)
	noerr(t, err)
	defer s.Close()
	st := s.Snapshot()
	require(t, st.Jobs["preflight"].Stage == "queued" && st.Jobs["preflight"].AccountID == "", "preflight deadlock")
	for _, k := range []string{"creating", "draft", "submitting"} {
		require(t, st.Jobs[k].Stage == "submission_unknown", k)
	}
	require(t, st.Jobs["pending"].Stage == "pending", "pending lost")
}
func TestNormalizeAndIdempotency(t *testing.T) {
	e, _ := fixtureEngine(t, 0)
	in := group()
	in.Name = ""
	a, err := e.Enqueue("alice", "default-name", in)
	noerr(t, err)
	b, err := e.Enqueue("alice", "default-name", in)
	noerr(t, err)
	require(t, a.ID == b.ID && a.Input.Name != "", "defaults")
	in = group()
	in.Sequences[0].Copies = 3
	_, err = e.Enqueue("alice", "default-name", in)
	require(t, err != nil && err.Error() == "IDEMPOTENCY_CONFLICT", err)
	for _, x := range []Input{{Name: "bad"}, {Sequences: []Sequence{{"abc", 1}}}, {Sequences: []Sequence{{"TEACHINGS", 1000}}}, {Sequences: []Sequence{{"TEACHINGS", -1}}}} {
		_, err := Normalize(x)
		require(t, err != nil, x)
	}
}
func TestWorkerConcurrentJobs(t *testing.T) {
	e, f := fixtureEngine(t, 1)
	a, err := e.Enqueue("alice", "one", group())
	noerr(t, err)
	b, err := e.Enqueue("bob", "two", group())
	noerr(t, err)
	var wg sync.WaitGroup
	for _, id := range []string{a.ID, b.ID} {
		wg.Add(1)
		go func(id string) { defer wg.Done(); e.process(context.Background(), id) }(id)
	}
	wg.Wait()
	require(t, f.count("kMnDgb") == 1, "same account assigned twice")
}

func request(s *Server, token, method, path, body, origin, csrf string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:8788"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func TestHTTPAuthorizationAndIsolation(t *testing.T) {
	e, _ := fixtureEngine(t, 0)
	tokens := map[string]string{}
	noerr(t, e.Store.Update(func(s *State) error {
		for id, u := range s.Users {
			tokens[id] = ID() + ID()
			u.TokenHash = tokenHash(tokens[id])
			s.Users[id] = u
		}
		return nil
	}))
	s, err := NewServer(e, "http://127.0.0.1:8788")
	noerr(t, err)
	j, err := e.Enqueue("alice", "private", group())
	noerr(t, err)
	cases := []struct {
		user, method, path, body string
		code                     int
	}{{"", "GET", "/api/v1/jobs", "", 403}, {"bob", "GET", "/api/v1/jobs/" + j.ID, "", 404}, {"bob", "GET", "/api/v1/jobs/" + j.ID + "/download", "", 404}, {"bob", "POST", "/api/v1/jobs/" + j.ID + "/check", "{}", 404}, {"alice", "GET", "/api/v1/jobs/" + j.ID, "", 200}, {"admin", "GET", "/api/v1/jobs/" + j.ID, "", 200}, {"bob", "GET", "/api/v1/admin/accounts", "", 403}, {"bob", "GET", "/api/v1/admin/users", "", 403}, {"bob", "GET", "/api/v1/admin/audit", "", 403}, {"bob", "POST", "/api/v1/admin/accounts", "{}", 403}, {"admin", "GET", "/api/v1/admin/accounts", "", 200}, {"alice", "POST", "/api/v1/jobs", `{"unexpected":1}`, 400}}
	for _, c := range cases {
		w := request(s, tokens[c.user], c.method, c.path, c.body, "", "")
		require(t, w.Code == c.code, c, w.Code, w.Body.String())
	}
	w := request(s, tokens["bob"], "GET", "/api/v1/jobs", "", "", "")
	require(t, strings.TrimSpace(w.Body.String()) == "[]", w.Body.String())
	w = request(s, tokens["alice"], "GET", "/api/v1/jobs", "", "https://evil.example", "")
	require(t, w.Code == 403, "origin")
	r := httptest.NewRequest("GET", "http://evil.example/", nil)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	require(t, w.Code == 403, "host")
	w = request(s, tokens["admin"], "POST", "/api/v1/admin/users/bob", `{"disabled":true}`, "", "")
	require(t, w.Code == 200, w.Body.String())
	w = request(s, tokens["bob"], "GET", "/api/v1/jobs", "", "", "")
	require(t, w.Code == 403, "token not revoked")
}
func TestPasswordSessionsCSRFAndEmbeddedUI(t *testing.T) {
	e, _ := fixtureEngine(t, 0)
	_, err := CreateUser(e.Store, "member", "correct-password-123", "member", "test")
	noerr(t, err)
	s, err := NewServer(e, "http://127.0.0.1:8788")
	noerr(t, err)
	origin := s.Origin
	w := request(s, "", "POST", "/api/v1/session", `{"name":"member","password":"wrong-password"}`, origin, "")
	require(t, w.Code == 403, "wrong password")
	w = request(s, "", "POST", "/api/v1/session", `{"name":"member","password":"correct-password-123"}`, origin, "")
	require(t, w.Code == 200, w.Body.String())
	cookies := w.Result().Cookies()
	require(t, len(cookies) == 1 && cookies[0].HttpOnly && cookies[0].SameSite == http.SameSiteStrictMode, "cookie")
	var body struct {
		CSRF string `json:"csrf"`
	}
	noerr(t, json.Unmarshal(w.Body.Bytes(), &body))
	for _, valid := range []bool{false, true} {
		r := httptest.NewRequest("POST", origin+"/api/v1/token", strings.NewReader("{}"))
		r.AddCookie(cookies[0])
		r.Header.Set("Origin", origin)
		if valid {
			r.Header.Set("X-CSRF-Token", body.CSRF)
		}
		w = httptest.NewRecorder()
		s.ServeHTTP(w, r)
		require(t, (valid && w.Code == 201) || (!valid && w.Code == 403), w.Code)
	}
	for _, p := range []string{"/", "/assets/app.js", "/assets/style.css"} {
		w = request(s, "", "GET", p, "", "", "")
		require(t, w.Code == 200 && w.Body.Len() > 100 && strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'"), p)
	}
}
func TestRealChromeSmoke(t *testing.T) {
	if os.Getenv("AF_TEST_BROWSER") != "1" {
		t.Skip("set AF_TEST_BROWSER=1 for isolated real browser smoke")
	}
	exe, err := FindChrome("")
	noerr(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	profile := filepath.Join(t.TempDir(), "profile")
	b, err := startChrome(ctx, profile, exe, false, "about:blank")
	noerr(t, err)
	defer b.Close()
	var n int
	noerr(t, b.Eval(ctx, "1+1", &n))
	require(t, n == 2, "CDP evaluation")
	noerr(t, b.Close())
	select {
	case <-b.done:
	default:
		t.Fatal("browser still running")
	}
	b, err = startChrome(ctx, profile, exe, false, "about:blank")
	noerr(t, err)
	defer b.Close()
	noerr(t, b.Eval(ctx, "1+1", &n))
	noerr(t, b.Close())
	t.Log("real Chrome: isolated profile launch -> CDP=2 -> close -> same-profile relaunch -> CDP=2 -> close; no Google login or prediction")
}
