package platform

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBatchAtomicAndIdempotent(t *testing.T) {
	e, _ := fixtureEngine(t, 0)
	items := []BatchItem{{"one", group()}, {"two", group()}}
	first, err := e.EnqueueBatch("alice", items)
	noerr(t, err)
	again, err := e.EnqueueBatch("alice", items)
	noerr(t, err)
	require(t, len(e.Store.Snapshot().Jobs) == 2 && first[0].ID == again[0].ID && len(first[0].Input.Sequences) == 2, "grouping or retry")
	bad := []BatchItem{{"three", group()}, {"one", Input{Name: "changed", Sequences: group().Sequences}}}
	_, err = e.EnqueueBatch("alice", bad)
	require(t, err != nil && err.Error() == "IDEMPOTENCY_CONFLICT", "conflict")
	require(t, len(e.Store.Snapshot().Jobs) == 2, "partial commit")
	bad[1] = BatchItem{"four", Input{}}
	_, err = e.EnqueueBatch("alice", bad)
	require(t, err != nil && len(e.Store.Snapshot().Jobs) == 2, "invalid atomic")
	_, err = e.EnqueueBatch("alice", []BatchItem{{"one", group()}, {"one", group()}})
	require(t, err != nil, "duplicate id")
	_, err = e.EnqueueBatch("bob", items)
	noerr(t, err)
	_, err = e.GetJob(User{ID: "bob", Role: "member"}, first[0].ID)
	require(t, err != nil, "owner isolation")
	// Empty-name retries resolve the prior name while the transaction is locked.
	in := group()
	in.Name = ""
	a, err := e.Enqueue("alice", "empty-name", in)
	noerr(t, err)
	b, err := e.Enqueue("alice", "empty-name", in)
	noerr(t, err)
	require(t, a.ID == b.ID, "empty name retry")
	for i := 0; i < 46; i++ {
		_, err = e.Enqueue("alice", fmt.Sprint("fill-", i), group())
		noerr(t, err)
	}
	_, err = e.EnqueueBatch("alice", []BatchItem{{"last-1", group()}, {"last-2", group()}})
	require(t, err != nil && err.Error() == "QUEUE_FULL" && len(e.Store.Snapshot().Jobs) == 51, "queue atomicity")
}
func TestBatchConcurrentRetry(t *testing.T) {
	e, _ := fixtureEngine(t, 0)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := e.EnqueueBatch("alice", []BatchItem{{"same", group()}, {"other", group()}})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	require(t, len(e.Store.Snapshot().Jobs) == 2, "concurrent duplicates")
}
func TestImportStrictMultipleFiles(t *testing.T) {
	official := `[{"name":"complex","modelSeeds":[],"sequences":[{"proteinChain":{"sequence":"TEACHINGS","count":2}},{"proteinChain":{"sequence":"ACDEFG","count":1}}],"dialect":"alphafoldserver","version":1}]`
	local, _ := json.Marshal(group())
	rows, issues := ImportInputs([]ImportFile{{"server.json", json.RawMessage(official)}, {"local.json", local}})
	require(t, len(issues) == 0 && len(rows) == 2 && len(rows[0].Input.Sequences) == 2 && rows[0].Input.Sequences[0].Copies == 2, "multi-file grouping")
	for _, raw := range []string{`[]`, `null`, `{"name":"x","sequences":[],"ignored":true}`, strings.Replace(official, `"modelSeeds":[]`, `"modelSeeds":["42"]`, 1), strings.Replace(official, `"count":2`, `"count":2,"modifications":[]`, 1), strings.Replace(official, `"proteinChain"`, `"dnaSequence"`, 1), strings.Replace(official, `"version":1`, `"version":3`, 1)} {
		rows, issues = ImportInputs([]ImportFile{{"valid.json", local}, {"bad.json", json.RawMessage(raw)}})
		require(t, len(rows) == 0 && len(issues) > 0, "unsupported import was silently accepted: "+raw)
	}
}
func TestWebSetupAtomicAndProtected(t *testing.T) {
	store := newTestStore(t)
	pool := NewBrowserPool(1, time.Minute, nil)
	defer pool.Close()
	s, err := NewServer(NewEngine(store, pool), "http://localhost")
	noerr(t, err)
	token, err := os.ReadFile(filepath.Join(store.Dir, "setup-token.txt"))
	noerr(t, err)
	call := func(method, origin, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://localhost/api/v1/setup", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	status := call("GET", "", "")
	require(t, status.Code == 200 && strings.Contains(status.Body.String(), `"required":true`) && !strings.Contains(status.Body.String(), strings.TrimSpace(string(token))), "setup status leaked token")
	data := func(tok string) string {
		b, _ := json.Marshal(map[string]string{"name": "first-admin", "password": "valid-password-1234", "token": tok})
		return string(b)
	}
	require(t, call("POST", "http://evil.example", data(string(token))).Code == 403, "cross-origin setup")
	require(t, call("POST", "http://localhost", data("wrong")).Code == 403, "wrong token")
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- call("POST", "http://localhost", data(strings.TrimSpace(string(token)))).Code
		}()
	}
	wg.Wait()
	close(codes)
	success := 0
	for code := range codes {
		if code == 201 {
			success++
		} else {
			require(t, code == 409, code)
		}
	}
	require(t, success == 1 && len(store.Snapshot().Users) == 1, "setup race")
	require(t, call("POST", "http://localhost", data(strings.TrimSpace(string(token)))).Code == 409, "setup replay")
	require(t, strings.Contains(call("GET", "", "").Body.String(), `"required":false`), "setup not disabled")
	_, err = os.Stat(filepath.Join(store.Dir, "setup-token.txt"))
	require(t, os.IsNotExist(err), "token not removed")
	for _, u := range store.Snapshot().Users {
		require(t, u.PasswordHash != "valid-password-1234" && passwordOK(u.PasswordHash, "valid-password-1234"), "password storage")
	}
}
