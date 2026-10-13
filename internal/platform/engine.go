package platform

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Engine struct {
	Store    *Store
	Browsers *BrowserPool
	mu       sync.Mutex
	jobs     map[string]bool
	accounts map[string]bool
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

func NewEngine(s *Store, p *BrowserPool) *Engine {
	return &Engine{Store: s, Browsers: p, jobs: map[string]bool{}, accounts: map[string]bool{}}
}
func (e *Engine) Enqueue(owner, request string, input Input) (Job, error) {
	if request == "" {
		request = ID()
	}
	jobs, err := e.EnqueueBatch(owner, []BatchItem{{RequestID: request, Input: input}})
	if err != nil {
		return Job{}, err
	}
	return jobs[0], nil
}
func terminal(stage string) bool {
	return stage == "succeeded" || stage == "failed" || stage == "preflight_failed" || stage == "cancelled"
}
func (e *Engine) GetJob(user User, id string) (Job, error) {
	j, ok := e.Store.Snapshot().Jobs[id]
	if !ok || (user.Role != "admin" && j.Owner != user.ID) {
		return Job{}, fail("NOT_FOUND")
	}
	return j, nil
}
func (e *Engine) Start(workers int) {
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	for i := 0; i < workers; i++ {
		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if id := e.claim(); id != "" {
						func() {
							defer func() { e.mu.Lock(); delete(e.jobs, id); e.mu.Unlock() }()
							runCtx, c := context.WithTimeout(ctx, 2*time.Minute)
							defer c()
							e.process(runCtx, id)
						}()
					}
					e.Browsers.Sweep()
				}
			}
		}()
	}
}
func (e *Engine) Stop() {
	if e.cancel != nil {
		e.cancel()
	}
	e.wg.Wait()
	e.Browsers.Close()
}
func (e *Engine) claim() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.Store.Snapshot()
	if s.CooldownUntil > stamp() {
		return ""
	}
	list := []Job{}
	for _, j := range s.Jobs {
		if !e.jobs[j.ID] && j.NextAt <= stamp() && (j.Stage == "queued" || j.Stage == "waiting_for_account" || j.Stage == "pending") {
			list = append(list, j)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].NextAt < list[j].NextAt || list[i].NextAt == list[j].NextAt && list[i].CreatedAt < list[j].CreatedAt
	})
	if len(list) == 0 {
		return ""
	}
	e.jobs[list[0].ID] = true
	return list[0].ID
}
func (e *Engine) reserve(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.accounts[id] {
		return false
	}
	e.accounts[id] = true
	return true
}
func (e *Engine) release(id string) { e.mu.Lock(); delete(e.accounts, id); e.mu.Unlock() }
func (e *Engine) updateJob(id string, fn func(*Job)) error {
	return e.Store.Update(func(s *State) error {
		j, ok := s.Jobs[id]
		if !ok {
			return fail("NOT_FOUND")
		}
		fn(&j)
		j.UpdatedAt = stamp()
		s.Jobs[id] = j
		return nil
	})
}
func (e *Engine) identity(ctx context.Context, b Browser, id string) error {
	r, err := RPC(ctx, b, "EU26qc", []any{}, "identity")
	if err != nil {
		return err
	}
	if len(r.Identity) != 64 {
		return fail("PROTOCOL_CHANGED")
	}
	return e.Store.Update(func(s *State) error {
		a, ok := s.Accounts[id]
		if !ok {
			return fail("ACCOUNT_NOT_FOUND")
		}
		if a.Identity != "" && a.Identity != r.Identity {
			return fail("ACCOUNT_CHANGED")
		}
		for other, b := range s.Accounts {
			if other != id && b.Identity == r.Identity {
				return fail("DUPLICATE_SESSION")
			}
		}
		a.Identity = r.Identity
		s.Accounts[id] = a
		return nil
	})
}
func (e *Engine) accountError(id string, err error) {
	_ = e.Store.Update(func(s *State) error {
		a := s.Accounts[id]
		a.Status = "connection_error"
		a.LastError = safeCode(err)
		a.RetryAt = stamp() + 30000
		a.QueueOpen = false
		if a.LastError == "NEEDS_LOGIN" {
			a.Status = "needs_login"
		}
		if a.LastError == "ACCOUNT_CHANGED" || a.LastError == "DUPLICATE_SESSION" {
			a.Enabled = false
			a.Status = "identity_mismatch"
		}
		if a.LastError == "RATE_LIMITED" {
			s.CooldownUntil = stamp() + 60000
			a.Status = "cooldown"
		}
		s.Accounts[id] = a
		return nil
	})
}
func safeCode(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if len(s) == 0 || len(s) > 80 {
		return "INTERNAL_OR_TRANSPORT_ERROR"
	}
	for _, c := range s {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return "INTERNAL_OR_TRANSPORT_ERROR"
		}
	}
	return s
}
func (e *Engine) refreshOn(ctx context.Context, b Browser, id string) (Account, error) {
	if err := waitPlatform(ctx, b); err != nil {
		return Account{}, err
	}
	if err := e.identity(ctx, b, id); err != nil {
		return Account{}, err
	}
	r, err := RPC(ctx, b, "tjI49c", []any{}, "")
	if err != nil {
		return Account{}, err
	}
	row := arr(at(r.Payload, 0))
	remaining := num(at(row, 0))
	open, ok := at(row, 1).(bool)
	if remaining < 0 || !ok {
		return Account{}, fail("PROTOCOL_CHANGED")
	}
	var out Account
	err = e.Store.Update(func(s *State) error {
		a := s.Accounts[id]
		a.Remaining = &remaining
		a.QueueOpen = open
		a.CheckedAt = stamp()
		a.RetryAt = 0
		a.LastError = ""
		a.Status = "ready"
		if remaining == 0 {
			a.Status = "quota_exhausted"
			a.RetryAt = stamp() + 3600000
		} else if !open {
			a.Status = "queue_closed"
			a.RetryAt = stamp() + 60000
		}
		s.Accounts[id] = a
		out = a
		return nil
	})
	return out, err
}
func (e *Engine) candidates() []Account {
	s := e.Store.Snapshot()
	busy := map[string]bool{}
	for _, j := range s.Jobs {
		if j.AccountID != "" && !terminal(j.Stage) {
			busy[j.AccountID] = true
		}
	}
	out := []Account{}
	for _, a := range s.Accounts {
		if a.Enabled && !busy[a.ID] && a.Status != "needs_login" && a.RetryAt <= stamp() {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		q := func(a Account) int {
			if a.Remaining == nil {
				return -1
			}
			return *a.Remaining
		}
		if q(out[i]) != q(out[j]) {
			return q(out[i]) > q(out[j])
		}
		return out[i].ID < out[j].ID
	})
	return out
}
func (e *Engine) process(ctx context.Context, id string) {
	j := e.Store.Snapshot().Jobs[id]
	if j.Stage == "pending" {
		e.poll(ctx, j)
		return
	}
	if j.Stage != "queued" && j.Stage != "waiting_for_account" {
		return
	}
	if e.Store.Snapshot().Users[j.Owner].Disabled {
		_ = e.updateJob(id, func(x *Job) { x.Stage = "cancelled"; x.Error = "OWNER_DISABLED" })
		return
	}
	// Check only candidates until one succeeds; never refresh the whole account pool per submission.
	for _, a := range e.candidates() {
		if ctx.Err() != nil {
			return
		}
		if !e.reserve(a.ID) {
			continue
		}
		selected := false
		func() {
			defer e.release(a.ID)
			eligible := false
			for _, current := range e.candidates() {
				if current.ID == a.ID {
					eligible = true
					break
				}
			}
			if !eligible {
				return
			}
			lease, err := e.Browsers.Acquire(ctx, a.ID, false)
			if err != nil {
				e.accountError(a.ID, err)
				return
			}
			defer lease.Release()
			fresh, err := e.refreshOn(ctx, lease.Browser, a.ID)
			if err != nil {
				lease.Discard()
				e.accountError(a.ID, err)
				return
			}
			if fresh.Status != "ready" {
				return
			}
			if e.Store.Snapshot().CooldownUntil > stamp() {
				return
			}
			selected = true
			e.submitOn(ctx, j, a.ID, lease.Browser)
			if e.Store.Snapshot().Accounts[a.ID].Status == "connection_error" {
				lease.Discard()
			}
		}()
		if selected {
			return
		}
		if e.Store.Snapshot().CooldownUntil > stamp() {
			break
		}
	}
	_ = e.updateJob(id, func(j *Job) {
		j.Stage = "waiting_for_account"
		j.NextAt = stamp() + 30000
		j.Error = "NO_VERIFIED_ACCOUNT_AVAILABLE"
	})
}
func (e *Engine) submitOn(ctx context.Context, j Job, account string, b Browser) {
	if err := e.updateJob(j.ID, func(x *Job) { x.AccountID = account; x.Stage = "preflight"; x.Error = "" }); err != nil {
		return
	}
	fold := foldInput(j.Input)
	_, err := RPC(ctx, b, "Gbje0", []any{fold}, "")
	if err == nil {
		err = e.identity(ctx, b, account)
	}
	if err != nil {
		e.accountError(account, err)
		_ = e.updateJob(j.ID, func(x *Job) { x.Stage = "preflight_failed"; x.Error = safeCode(err) })
		return
	}
	if err = e.Store.Update(func(s *State) error {
		if !s.Accounts[account].Enabled || s.Users[j.Owner].Disabled || s.CooldownUntil > stamp() {
			x := s.Jobs[j.ID]
			x.Stage = "preflight_failed"
			x.Error = "SUBMISSION_PAUSED"
			s.Jobs[j.ID] = x
			return fail("SUBMISSION_PAUSED")
		}
		x := s.Jobs[j.ID]
		x.Stage = "creating"
		x.UpdatedAt = stamp()
		s.Jobs[j.ID] = x
		return nil
	}); err != nil {
		_ = e.updateJob(j.ID, func(x *Job) { x.Stage = "preflight_failed"; x.Error = safeCode(err) })
		return
	}
	draft, err := RPC(ctx, b, "kMnDgb", []any{fold, 5}, "")
	remote := str(at(arr(at(draft.Payload, 0)), 0))
	if err == nil && (remote == "" || num(at(draft.Payload, 1)) != 5) {
		err = fail("PROTOCOL_CHANGED")
	}
	if err != nil {
		e.unknown(j.ID, account, err)
		return
	}
	var seedBytes [4]byte
	if _, err = rand.Read(seedBytes[:]); err != nil {
		e.unknown(j.ID, account, err)
		return
	}
	seed := binary.LittleEndian.Uint32(seedBytes[:])
	if err = e.updateJob(j.ID, func(x *Job) { x.RemoteID = remote; x.Seed = seed; x.Stage = "draft" }); err != nil {
		return
	}
	update := []any{[]any{remote}, 1, nil, nil, nil, nil, fold, nil, nil, nil, nil, nil, nil, []any{seed}}
	if err = e.updateJob(j.ID, func(x *Job) { x.Stage = "submitting" }); err != nil {
		return
	}
	result, err := RPC(ctx, b, "bteK7", []any{update, []any{[]any{"fold_job_status", "fold_input", "model_seeds"}}}, "")
	row := arr(at(result.Payload, 0))
	status := num(at(row, 1))
	if err == nil && (str(at(arr(at(row, 0)), 0)) != remote || (status != 1 && status != 3 && status != 4)) {
		err = fail("PROTOCOL_CHANGED")
	}
	if err != nil {
		e.unknown(j.ID, account, err)
		return
	}
	_ = e.Store.Update(func(s *State) error {
		x := s.Jobs[j.ID]
		x.Stage = map[int]string{1: "pending", 3: "succeeded", 4: "failed"}[status]
		x.UpdatedAt = stamp()
		x.NextAt = stamp() + 30000
		x.Error = ""
		s.Jobs[j.ID] = x
		a := s.Accounts[account]
		if a.Remaining != nil && *a.Remaining > 0 {
			n := *a.Remaining - 1
			a.Remaining = &n
		}
		s.Accounts[account] = a
		s.log(x.Owner, "job.submitted", j.ID)
		return nil
	})
}
func (e *Engine) unknown(id, account string, err error) {
	e.accountError(account, err)
	_ = e.updateJob(id, func(j *Job) { j.Stage = "submission_unknown"; j.Error = safeCode(err); j.NextAt = 0 })
}
func (e *Engine) poll(ctx context.Context, j Job) {
	if e.Store.Snapshot().CooldownUntil > stamp() {
		return
	}
	if !e.reserve(j.AccountID) {
		_ = e.updateJob(j.ID, func(x *Job) { x.NextAt = stamp() + 5000 })
		return
	}
	defer e.release(j.AccountID)
	lease, err := e.Browsers.Acquire(ctx, j.AccountID, false)
	if err == nil {
		defer lease.Release()
		err = waitPlatform(ctx, lease.Browser)
	}
	if err == nil {
		err = e.identity(ctx, lease.Browser, j.AccountID)
	}
	var r RPCResult
	if err == nil {
		r, err = RPC(ctx, lease.Browser, "EXvXWc", []any{[]any{j.RemoteID}}, "")
	}
	status := num(at(r.Payload, 1))
	if err == nil && (str(at(arr(at(r.Payload, 0)), 0)) != j.RemoteID || (status != 1 && status != 3 && status != 4 && status != 5)) {
		err = fail("PROTOCOL_CHANGED")
	}
	if err != nil {
		if lease != nil {
			lease.Discard()
		}
		e.accountError(j.AccountID, err)
		_ = e.updateJob(j.ID, func(x *Job) { x.Error = safeCode(err); x.NextAt = stamp() + 60000 })
		return
	}
	_ = e.updateJob(j.ID, func(x *Job) {
		x.Stage = map[int]string{1: "pending", 3: "succeeded", 4: "failed", 5: "draft_attention"}[status]
		x.Models = len(arr(at(r.Payload, 7)))
		x.Error = ""
		x.NextAt = stamp() + 30000
	})
}
func (e *Engine) Download(ctx context.Context, user User, id string) (string, error) {
	j, err := e.GetJob(user, id)
	if err != nil {
		return "", err
	}
	if j.Stage != "succeeded" {
		return "", fail("JOB_NOT_SUCCEEDED")
	}
	out := filepath.Join(e.Store.Dir, "downloads", j.ID+".zip")
	if j.DownloadSHA != "" {
		b, err := os.ReadFile(out)
		if err == nil {
			sum := sha256.Sum256(b)
			if hex.EncodeToString(sum[:]) == j.DownloadSHA {
				return out, nil
			}
		}
	}
	if e.Store.Snapshot().CooldownUntil > stamp() {
		return "", fail("GLOBAL_COOLDOWN")
	}
	if !e.reserve(j.AccountID) {
		return "", fail("ACCOUNT_BUSY")
	}
	defer e.release(j.AccountID)
	l, err := e.Browsers.Acquire(ctx, j.AccountID, false)
	if err != nil {
		return "", err
	}
	defer l.Release()
	if err = waitPlatform(ctx, l.Browser); err != nil {
		return "", err
	}
	if err = e.identity(ctx, l.Browser, j.AccountID); err != nil {
		return "", err
	}
	b, err := zipBytes(ctx, l.Browser, j.RemoteID)
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(filepath.Dir(out), 0700); err != nil {
		return "", err
	}
	if err = atomicFile(out, b); err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	if err = e.updateJob(j.ID, func(x *Job) { x.DownloadSHA = hex.EncodeToString(sum[:]) }); err != nil {
		return "", err
	}
	return out, nil
}
func (e *Engine) AccountAction(ctx context.Context, id, action string) error {
	if _, ok := e.Store.Snapshot().Accounts[id]; !ok {
		return fail("ACCOUNT_NOT_FOUND")
	}
	if !e.reserve(id) {
		return fail("ACCOUNT_BUSY")
	}
	defer e.release(id)
	if action == "sleep" {
		return e.Browsers.Stop(id)
	}
	if action != "login" && e.Store.Snapshot().CooldownUntil > stamp() {
		return fail("GLOBAL_COOLDOWN")
	}
	if action == "login" {
		for _, j := range e.Store.Snapshot().Jobs {
			if j.AccountID == id && !terminal(j.Stage) {
				return fail("ACCOUNT_HAS_ACTIVE_JOB")
			}
		}
	}
	// Interactive authentication is never controlled through CDP. Verification
	// starts separately, after the administrator closes the ordinary browser.
	headed := action == "login"
	l, err := e.Browsers.Acquire(ctx, id, headed)
	if err != nil {
		if err.Error() != "LOGIN_WINDOW_OPEN" {
			e.accountError(id, err)
		}
		return err
	}
	if action == "login" {
		for attempt := 0; ; attempt++ {
			presenter, ok := l.Browser.(interface{ ShowLogin(context.Context) error })
			if !ok {
				err = fail("BROWSER_LOGIN_UNSUPPORTED")
			} else {
				err = presenter.ShowLogin(ctx)
			}
			if err == nil {
				break
			}
			// A native window can close before Alive observes process exit.
			// Retry only local window presentation, once, using the same profile;
			// never retry authentication, quota requests, or prediction writes here.
			reopen := err.Error() == "BROWSER_DISCONNECTED" || err.Error() == "BROWSER_DISCONNECTED_OR_TIMEOUT" || err.Error() == "LOGIN_WINDOW_UNAVAILABLE"
			l.Discard()
			if attempt > 0 || !reopen || ctx.Err() != nil {
				e.accountError(id, err)
				return err
			}
			l, err = e.Browsers.Acquire(ctx, id, true)
			if err != nil {
				e.accountError(id, err)
				return err
			}
		}
		l.Release()
		return e.Store.Update(func(s *State) error {
			a := s.Accounts[id]
			a.Status = "needs_login"
			a.LastError = ""
			a.RetryAt = 0
			s.Accounts[id] = a
			return nil
		})
	}
	_, err = e.refreshOn(ctx, l.Browser, id)
	if err != nil || action == "finish-login" {
		l.Discard()
	} else {
		l.Release()
	}
	if err != nil {
		e.accountError(id, err)
	}
	return err
}
