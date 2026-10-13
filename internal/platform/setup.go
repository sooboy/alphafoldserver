package platform

import (
	"crypto/subtle"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// An operator-held token prevents an internet visitor from claiming a fresh
// installation. It is never sent by the unauthenticated setup-status endpoint.
func prepareSetup(s *Store) (string, error) {
	if len(s.Snapshot().Users) > 0 {
		return "", nil
	}
	path := filepath.Join(s.Dir, "setup-token.txt")
	b, err := os.ReadFile(path)
	if err == nil {
		token := strings.TrimSpace(string(b))
		if len(token) != 64 {
			return "", fail("INVALID_SETUP_TOKEN_FILE")
		}
		return token, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	token := ID() + ID()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	_, err = f.WriteString(token + "\n")
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	return token, closeErr
}
func InitializeAdmin(s *Store, name, password string) (User, error) {
	if !validID.MatchString(name) {
		return User{}, fail("INVALID_USER")
	}
	hash, err := passwordHash(password)
	if err != nil {
		return User{}, err
	}
	user := User{ID: ID(), Name: name, Role: "admin", PasswordHash: hash}
	err = s.Update(func(st *State) error {
		if len(st.Users) != 0 {
			return fail("ALREADY_INITIALIZED")
		}
		st.Users[user.ID] = user
		st.log(user.ID, "platform.initialize", user.ID)
		return nil
	})
	return user, err
}
func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	required := len(s.Engine.Store.Snapshot().Users) == 0
	if r.Method == "GET" {
		jsonOut(w, 200, map[string]bool{"required": required, "token_required": required})
		return
	}
	if r.Method != "POST" {
		errorOut(w, fail("NOT_FOUND"))
		return
	}
	if r.Header.Get("Origin") != s.Origin {
		errorOut(w, fail("FORBIDDEN"))
		return
	}
	if !required {
		errorOut(w, fail("ALREADY_INITIALIZED"))
		return
	}
	if !s.allowedLogin(r) {
		errorOut(w, fail("RATE_LIMITED"))
		return
	}
	var body struct {
		Name     string `json:"name"`
		Password string `json:"password"`
		Token    string `json:"token"`
	}
	if err := decode(w, r, &body); err != nil {
		errorOut(w, err)
		return
	}
	if s.setupToken == "" || subtle.ConstantTimeCompare([]byte(s.setupToken), []byte(body.Token)) != 1 {
		errorOut(w, fail("FORBIDDEN"))
		return
	}
	user, err := InitializeAdmin(s.Engine.Store, body.Name, body.Password)
	if err != nil {
		errorOut(w, err)
		return
	}
	// State, not deletion, invalidates setup; a failure to remove cannot reopen it.
	_ = os.Remove(filepath.Join(s.Engine.Store.Dir, "setup-token.txt"))
	jsonOut(w, 201, map[string]any{"initialized": true, "user": publicUser(user)})
}
