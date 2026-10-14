package platform

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Real HTTP handlers, persisted local store, fake upstream browser only.
// The JSON bodies are the same files linked from docs/API_USAGE.zh-CN.md.
func TestDocumentedAPI(t *testing.T) {
	e, remote := fixtureEngine(t, 1)
	tokens := map[string]string{}
	noerr(t, e.Store.Update(func(st *State) error {
		for id, u := range st.Users {
			tokens[id] = ID() + ID()
			u.TokenHash = tokenHash(tokens[id])
			st.Users[id] = u
		}
		return nil
	}))
	s, err := NewServer(e, "http://127.0.0.1:8788")
	noerr(t, err)
	read := func(name string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join("..", "..", "examples", "api", name))
		noerr(t, err)
		require(t, json.Valid(b), "invalid example JSON")
		return string(b)
	}
	single := read("single-request.json")
	w := request(s, tokens["alice"], "POST", "/api/v1/jobs", single, "", "")
	require(t, w.Code == 200 && strings.Contains(w.Body.String(), `"no_submission":true`), w.Body.String())
	require(t, len(e.Store.Snapshot().Jobs) == 0 && remote.launches == 0, "preview has side effects")
	var singleBody map[string]any
	noerr(t, json.Unmarshal([]byte(single), &singleBody))
	singleBody["execute"] = true
	body, _ := json.Marshal(singleBody)
	w = request(s, tokens["alice"], "POST", "/api/v1/jobs", string(body), "", "")
	require(t, w.Code == 202, w.Body.String())
	var j Job
	noerr(t, json.Unmarshal(w.Body.Bytes(), &j))
	require(t, len(j.Input.Sequences) == 2 && j.Input.Sequences[1].Copies == 2, "grouping")
	w = request(s, tokens["alice"], "POST", "/api/v1/jobs", string(body), "", "")
	require(t, w.Code == 202 && len(e.Store.Snapshot().Jobs) == 1, "single retry duplicated")
	t.Log("single example: preview HTTP 200 with no side effects; submit/retry HTTP 202, same one 2-sequence job; Bearer-only authentication")
	batch := read("batch-request.json")
	w = request(s, tokens["alice"], "POST", "/api/v1/jobs/batch", batch, "", "")
	require(t, w.Code == 200 && len(e.Store.Snapshot().Jobs) == 1, "batch preview")
	var batchBody map[string]any
	noerr(t, json.Unmarshal([]byte(batch), &batchBody))
	batchBody["execute"] = true
	body, _ = json.Marshal(batchBody)
	for i := 0; i < 2; i++ {
		w = request(s, tokens["alice"], "POST", "/api/v1/jobs/batch", string(body), "", "")
		require(t, w.Code == 202 && len(e.Store.Snapshot().Jobs) == 3, w.Body.String())
	}
	t.Log("batch example: preview HTTP 200; submit/retry HTTP 202, 2 additional jobs only")
	imported := read("import-request.json")
	w = request(s, tokens["alice"], "POST", "/api/v1/import", imported, "", "")
	require(t, w.Code == 200, w.Body.String())
	var result struct {
		Items        []ImportedInput `json:"items"`
		NoSubmission bool            `json:"no_submission"`
	}
	noerr(t, json.Unmarshal(w.Body.Bytes(), &result))
	require(t, len(result.Items) == 2 && result.NoSubmission && len(e.Store.Snapshot().Jobs) == 3, "import submitted")
	t.Log("multi-file import example: HTTP 200, 2 normalized inputs, zero added jobs")
	w = request(s, strings.Repeat("0", 64), "GET", "/api/v1/jobs", "", "", "")
	require(t, w.Code == 403, "invalid Token accepted")
	w = request(s, tokens["bob"], "GET", "/api/v1/jobs/"+j.ID, "", "", "")
	require(t, w.Code == 404, "other member sees job")
	w = request(s, tokens["bob"], "GET", "/api/v1/jobs/"+j.ID+"/download", "", "", "")
	require(t, w.Code == 404, "other member download")
	w = request(s, tokens["alice"], "GET", "/api/v1/jobs/"+j.ID, "", "", "")
	require(t, w.Code == 200, "owner detail")
	w = request(s, tokens["alice"], "POST", "/api/v1/jobs/"+j.ID+"/check", "{}", "", "")
	require(t, w.Code == 400 && strings.Contains(w.Body.String(), "CANNOT_RECONCILE_WITHOUT_REMOTE_ID"), "missing remote ID not rejected")
	// A known fake upstream fixture completes one job so the actual download route
	// can be exercised. This is NOT a real AlphaFold prediction or scientific ZIP.
	e.process(context.Background(), j.ID)
	j, err = e.GetJob(User{ID: "alice", Role: "member"}, j.ID)
	noerr(t, err)
	w = request(s, tokens["alice"], "POST", "/api/v1/jobs/"+j.ID+"/check", "{}", "", "")
	require(t, w.Code == 200, w.Body.String())
	w = request(s, tokens["alice"], "GET", "/api/v1/jobs/"+j.ID+"/download", "", "", "")
	require(t, w.Code == 200 && strings.HasPrefix(w.Body.String(), "PK"), w.Code, w.Body.String())
	require(t, strings.Contains(w.Header().Get("Content-Disposition"), ".zip"), "download header")
	t.Log("detail HTTP 200; invalid Token 403; other-member detail/download 404; missing remote ID check 400; fake-upstream completion -> download HTTP 200 (PK bytes); real_google_submission=false")
}
