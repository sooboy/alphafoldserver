package platform

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed assets/index.html assets/app.js assets/style.css
var ui embed.FS

type session struct {
	User, CSRF string
	Expires    time.Time
}
type rate struct {
	Count int
	Until time.Time
}
type Server struct {
	setupToken string
	Engine     *Engine
	Origin     string
	host       string
	secure     bool
	mu         sync.Mutex
	sessions   map[string]session
	rates      map[string]rate
}

func NewServer(e *Engine, origin string) (*Server, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return nil, fail("INVALID_PUBLIC_URL")
	}
	token, err := prepareSetup(e.Store)
	if err != nil {
		return nil, err
	}
	return &Server{setupToken: token, Engine: e, Origin: origin, host: u.Host, secure: u.Scheme == "https", sessions: map[string]session{}, rates: map[string]rate{}}, nil
}
func jsonOut(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func errorOut(w http.ResponseWriter, err error) {
	code := safeCode(err)
	status := 400
	switch code {
	case "FORBIDDEN":
		status = 403
	case "NOT_FOUND":
		status = 404
	case "ALREADY_INITIALIZED", "IDEMPOTENCY_CONFLICT", "BROWSER_BUSY", "ACCOUNT_BUSY":
		status = 409
	case "RATE_LIMITED", "QUEUE_FULL":
		status = 429
	}
	var batch *BatchError
	if errors.As(err, &batch) {
		jsonOut(w, status, map[string]any{"error": code, "index": batch.Index})
		return
	}
	jsonOut(w, status, map[string]string{"error": code})
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return fail("JSON_REQUIRED")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return fail("INVALID_JSON")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fail("INVALID_JSON")
	}
	return nil
}
func publicUser(u User) map[string]any {
	return map[string]any{"id": u.ID, "name": u.Name, "role": u.Role, "disabled": u.Disabled}
}
func (s *Server) authenticate(r *http.Request) (User, session, bool) {
	st := s.Engine.Store.Snapshot()
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") && len(token) == 64 {
		h := tokenHash(token)
		for _, u := range st.Users {
			if !u.Disabled && u.TokenHash == h {
				return u, session{}, true
			}
		}
	}
	cookie, err := r.Cookie("af_platform_session")
	if err != nil {
		return User{}, session{}, false
	}
	s.mu.Lock()
	sess, ok := s.sessions[tokenHash(cookie.Value)]
	if ok && time.Now().After(sess.Expires) {
		delete(s.sessions, tokenHash(cookie.Value))
		ok = false
	}
	s.mu.Unlock()
	if !ok {
		return User{}, session{}, false
	}
	u, ok := st.Users[sess.User]
	if !ok || u.Disabled {
		return User{}, session{}, false
	}
	return u, sess, true
}
func (s *Server) allowedLogin(r *http.Request) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, v := range s.rates {
		if now.After(v.Until) {
			delete(s.rates, k)
		}
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	v := s.rates[ip]
	if v.Until.IsZero() {
		if len(s.rates) >= 4096 {
			return false
		}
		v.Until = now.Add(time.Minute)
	}
	v.Count++
	s.rates[ip] = v
	return v.Count <= 10
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != s.Origin {
		errorOut(w, fail("FORBIDDEN"))
		return
	}
	if !s.allowedLogin(r) {
		errorOut(w, fail("RATE_LIMITED"))
		return
	}
	var body struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := decode(w, r, &body); err != nil {
		errorOut(w, err)
		return
	}
	var user User
	for _, u := range s.Engine.Store.Snapshot().Users {
		if u.Name == body.Name {
			user = u
			break
		}
	}
	hash := user.PasswordHash
	if hash == "" {
		hash = strings.Repeat("00", 16) + ":" + strings.Repeat("00", 32)
	}
	valid := passwordOK(hash, body.Password)
	if !valid || user.ID == "" || user.Disabled {
		errorOut(w, fail("FORBIDDEN"))
		return
	}
	token := ID() + ID()
	sess := session{User: user.ID, CSRF: ID() + ID(), Expires: time.Now().Add(8 * time.Hour)}
	s.mu.Lock()
	for k, v := range s.sessions {
		if time.Now().After(v.Expires) {
			delete(s.sessions, k)
		}
	}
	if len(s.sessions) >= 1000 {
		s.mu.Unlock()
		errorOut(w, fail("RATE_LIMITED"))
		return
	}
	s.sessions[tokenHash(token)] = sess
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "af_platform_session", Value: token, Path: "/", HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteStrictMode, MaxAge: 28800})
	jsonOut(w, 200, map[string]any{"user": publicUser(user), "csrf": sess.CSRF})
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	if r.Host != s.host || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != s.Origin) {
		errorOut(w, fail("FORBIDDEN"))
		return
	}
	route := r.URL.Path
	if r.Method == "GET" && (route == "/" || route == "/assets/app.js" || route == "/assets/style.css") {
		file := "assets/index.html"
		mime := "text/html; charset=utf-8"
		if route != "/" {
			file = strings.TrimPrefix(route, "/")
			if strings.HasSuffix(route, ".js") {
				mime = "text/javascript; charset=utf-8"
			} else {
				mime = "text/css; charset=utf-8"
			}
		}
		b, err := ui.ReadFile(file)
		if err != nil {
			errorOut(w, fail("NOT_FOUND"))
			return
		}
		w.Header().Set("Content-Type", mime)
		_, _ = w.Write(b)
		return
	}
	if route == "/api/v1/setup" {
		s.setup(w, r)
		return
	}
	if route == "/api/v1/session" && r.Method == "POST" {
		s.login(w, r)
		return
	}
	user, sess, ok := s.authenticate(r)
	if !ok {
		errorOut(w, fail("FORBIDDEN"))
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" && sess.User != "" && (r.Header.Get("Origin") != s.Origin || r.Header.Get("X-CSRF-Token") != sess.CSRF) {
		errorOut(w, fail("FORBIDDEN"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	if route == "/api/v1/session" {
		if r.Method == "GET" {
			jsonOut(w, 200, map[string]any{"user": publicUser(user), "csrf": sess.CSRF})
			return
		}
		if r.Method == "DELETE" {
			if cookie, err := r.Cookie("af_platform_session"); err == nil {
				s.mu.Lock()
				delete(s.sessions, tokenHash(cookie.Value))
				s.mu.Unlock()
			}
			http.SetCookie(w, &http.Cookie{Name: "af_platform_session", Path: "/", Value: "", HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteStrictMode, MaxAge: -1})
			jsonOut(w, 200, map[string]bool{"logged_out": true})
			return
		}
	}
	if route == "/api/v1/token" && r.Method == "POST" {
		token := ID() + ID()
		err := s.Engine.Store.Update(func(st *State) error {
			u := st.Users[user.ID]
			u.TokenHash = tokenHash(token)
			st.Users[u.ID] = u
			st.log(user.ID, "token.rotate", u.ID)
			return nil
		})
		if err != nil {
			errorOut(w, err)
			return
		}
		jsonOut(w, 201, map[string]string{"token": token, "note": "Shown once; rotation revokes previous token"})
		return
	}
	if route == "/api/v1/meta" && r.Method == "GET" {
		jsonOut(w, 200, map[string]any{"service": "alphafold-go-platform", "api_scope": "team-platform", "visibility": "owner_and_admin", "browser_pool": map[string]any{"limit": s.Engine.Browsers.Stats()["limit"], "active": s.Engine.Browsers.Stats()["active"]}, "upstream": "https://alphafoldserver.com", "browser_required": true, "manual_seed_supported": false})
		return
	}
	if route == "/api/v1/jobs/batch" && r.Method == "POST" {
		var body struct {
			Items   []BatchItem `json:"items"`
			Execute bool        `json:"execute"`
		}
		if err := decode(w, r, &body); err != nil {
			errorOut(w, err)
			return
		}
		if !body.Execute {
			items, err := prepareBatch(s.Engine.Store.Snapshot(), user.ID, body.Items)
			if err != nil {
				errorOut(w, err)
				return
			}
			jsonOut(w, 200, map[string]any{"items": items, "no_submission": true})
			return
		}
		jobs, err := s.Engine.EnqueueBatch(user.ID, body.Items)
		if err != nil {
			errorOut(w, err)
			return
		}
		jsonOut(w, 202, map[string]any{"jobs": jobs})
		return
	}
	if route == "/api/v1/import" && r.Method == "POST" {
		var body struct {
			Files []ImportFile `json:"files"`
		}
		if err := decode(w, r, &body); err != nil {
			errorOut(w, err)
			return
		}
		items, issues := ImportInputs(body.Files)
		if len(issues) > 0 {
			jsonOut(w, 400, map[string]any{"error": "IMPORT_REJECTED", "issues": issues})
			return
		}
		jsonOut(w, 200, map[string]any{"items": items, "no_submission": true})
		return
	}
	if route == "/api/v1/jobs" {
		if r.Method == "GET" {
			rows := []Job{}
			for _, j := range s.Engine.Store.Snapshot().Jobs {
				if user.Role == "admin" || j.Owner == user.ID {
					rows = append(rows, j)
				}
			}
			sort.Slice(rows, func(i, j int) bool { return rows[i].CreatedAt > rows[j].CreatedAt })
			jsonOut(w, 200, rows)
			return
		}
		if r.Method == "POST" {
			var body struct {
				RequestID string `json:"request_id"`
				Input     Input  `json:"input"`
				Execute   bool   `json:"execute"`
			}
			if err := decode(w, r, &body); err != nil {
				errorOut(w, err)
				return
			}
			if !body.Execute {
				in, err := Normalize(body.Input)
				if err != nil {
					errorOut(w, err)
					return
				}
				jsonOut(w, 200, map[string]any{"stage": "validated", "no_submission": true, "input": in, "note": "Read-only validation; account selected lazily when queued"})
				return
			}
			job, err := s.Engine.Enqueue(user.ID, body.RequestID, body.Input)
			if err != nil {
				errorOut(w, err)
				return
			}
			jsonOut(w, 202, job)
			return
		}
	}
	if strings.HasPrefix(route, "/api/v1/jobs/") {
		parts := strings.Split(strings.TrimPrefix(route, "/api/v1/jobs/"), "/")
		id := parts[0]
		j, err := s.Engine.GetJob(user, id)
		if err != nil {
			errorOut(w, err)
			return
		}
		if len(parts) == 1 && r.Method == "GET" {
			jsonOut(w, 200, j)
			return
		}
		if len(parts) == 2 && parts[1] == "download" && r.Method == "GET" {
			file, err := s.Engine.Download(ctx, user, id)
			if err != nil {
				errorOut(w, err)
				return
			}
			w.Header().Set("Content-Disposition", `attachment; filename="`+id+`.zip"`)
			http.ServeFile(w, r, file)
			return
		}
		if len(parts) == 2 && parts[1] == "check" && r.Method == "POST" {
			if j.RemoteID == "" || (j.Stage != "submission_unknown" && j.Stage != "draft_attention" && j.Stage != "pending") {
				errorOut(w, fail("CANNOT_RECONCILE_WITHOUT_REMOTE_ID"))
				return
			}
			s.Engine.poll(ctx, j)
			updated, _ := s.Engine.GetJob(user, id)
			jsonOut(w, 200, updated)
			return
		}
	}
	if strings.HasPrefix(route, "/api/v1/admin/") {
		if user.Role != "admin" {
			errorOut(w, fail("FORBIDDEN"))
			return
		}
		s.admin(ctx, w, r, user)
		return
	}
	errorOut(w, fail("NOT_FOUND"))
}
func (s *Server) admin(ctx context.Context, w http.ResponseWriter, r *http.Request, user User) {
	route := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/")
	if route == "users" && r.Method == "GET" {
		rows := []map[string]any{}
		for _, u := range s.Engine.Store.Snapshot().Users {
			rows = append(rows, publicUser(u))
		}
		jsonOut(w, 200, rows)
		return
	}
	if route == "users" && r.Method == "POST" {
		var body struct {
			Name     string `json:"name"`
			Password string `json:"password"`
			Role     string `json:"role"`
		}
		if err := decode(w, r, &body); err != nil {
			errorOut(w, err)
			return
		}
		u, err := CreateUser(s.Engine.Store, body.Name, body.Password, body.Role, user.ID)
		if err != nil {
			errorOut(w, err)
			return
		}
		jsonOut(w, 201, publicUser(u))
		return
	}
	if strings.HasPrefix(route, "users/") && r.Method == "POST" {
		id := strings.TrimPrefix(route, "users/")
		var body struct {
			Disabled bool `json:"disabled"`
		}
		if err := decode(w, r, &body); err != nil {
			errorOut(w, err)
			return
		}
		err := s.Engine.Store.Update(func(st *State) error {
			u, ok := st.Users[id]
			if !ok {
				return fail("NOT_FOUND")
			}
			if id == user.ID {
				return fail("CANNOT_DISABLE_SELF")
			}
			u.Disabled = body.Disabled
			u.TokenHash = ""
			st.Users[id] = u
			st.log(user.ID, "user.enabled_changed", id)
			return nil
		})
		if err != nil {
			errorOut(w, err)
			return
		}
		s.mu.Lock()
		for token, sess := range s.sessions {
			if sess.User == id {
				delete(s.sessions, token)
			}
		}
		s.mu.Unlock()
		jsonOut(w, 200, map[string]bool{"ok": true})
		return
	}
	if route == "accounts" && r.Method == "GET" {
		rows := []Account{}
		for _, a := range s.Engine.Store.Snapshot().Accounts {
			a.Identity = ""
			rows = append(rows, a)
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
		jsonOut(w, 200, map[string]any{"accounts": rows, "browsers": s.Engine.Browsers.Stats()})
		return
	}
	if route == "accounts" && r.Method == "POST" {
		var body struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		}
		if err := decode(w, r, &body); err != nil {
			errorOut(w, err)
			return
		}
		if !validID.MatchString(body.ID) || len(body.Label) > 160 {
			errorOut(w, fail("INVALID_ACCOUNT"))
			return
		}
		err := s.Engine.Store.Update(func(st *State) error {
			if len(st.Accounts) >= 1000 {
				return fail("ACCOUNT_LIMIT")
			}
			if _, ok := st.Accounts[body.ID]; ok {
				return fail("DUPLICATE_ACCOUNT")
			}
			st.Accounts[body.ID] = Account{ID: body.ID, Label: body.Label, Enabled: true, Status: "needs_login"}
			st.log(user.ID, "account.add", body.ID)
			return nil
		})
		if err != nil {
			errorOut(w, err)
			return
		}
		jsonOut(w, 201, map[string]string{"id": body.ID})
		return
	}
	if strings.HasPrefix(route, "accounts/") && r.Method == "POST" {
		parts := strings.Split(strings.TrimPrefix(route, "accounts/"), "/")
		if len(parts) != 2 || !validID.MatchString(parts[0]) {
			errorOut(w, fail("NOT_FOUND"))
			return
		}
		id, action := parts[0], parts[1]
		var err error
		switch action {
		case "login", "finish-login", "refresh", "sleep":
			err = s.Engine.AccountAction(ctx, id, action)
		case "enable", "disable":
			err = s.Engine.Store.Update(func(st *State) error {
				a, ok := st.Accounts[id]
				if !ok {
					return fail("NOT_FOUND")
				}
				a.Enabled = action == "enable"
				st.Accounts[id] = a
				return nil
			})
		default:
			err = fail("NOT_FOUND")
		}
		if err != nil {
			errorOut(w, err)
			return
		}
		_ = s.Engine.Store.Update(func(st *State) error { st.log(user.ID, "account."+action, id); return nil })
		jsonOut(w, 200, map[string]string{"status": "ok", "account_id": id, "action": action})
		return
	}
	if route == "audit" && r.Method == "GET" {
		jsonOut(w, 200, s.Engine.Store.Snapshot().Audit)
		return
	}
	if path.Clean(route) != route {
		errorOut(w, fail("NOT_FOUND"))
		return
	}
	errorOut(w, fail("NOT_FOUND"))
}
