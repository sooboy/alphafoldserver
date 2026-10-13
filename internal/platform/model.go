package platform

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
)

type User struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Role         string `json:"role"`
	PasswordHash string `json:"password_hash"`
	Disabled     bool   `json:"disabled"`
	TokenHash    string `json:"token_hash,omitempty"`
}
type Account struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Enabled   bool   `json:"enabled"`
	Identity  string `json:"identity,omitempty"`
	Status    string `json:"status"`
	Remaining *int   `json:"remaining"`
	QueueOpen bool   `json:"queue_open"`
	CheckedAt int64  `json:"checked_at"`
	RetryAt   int64  `json:"retry_at,omitempty"`
	LastError string `json:"last_error,omitempty"`
}
type Sequence struct {
	Sequence string `json:"sequence"`
	Copies   int    `json:"copies"`
}
type Input struct {
	Name      string     `json:"name"`
	Sequences []Sequence `json:"sequences"`
}
type Job struct {
	ID          string `json:"id"`
	Owner       string `json:"owner"`
	RequestID   string `json:"request_id"`
	Input       Input  `json:"input"`
	Hash        string `json:"input_hash"`
	AccountID   string `json:"account_id,omitempty"`
	RemoteID    string `json:"remote_id,omitempty"`
	Stage       string `json:"stage"`
	Seed        uint32 `json:"seed"`
	Models      int    `json:"models"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
	NextAt      int64  `json:"next_at"`
	Error       string `json:"error,omitempty"`
	DownloadSHA string `json:"download_sha256,omitempty"`
}
type Audit struct {
	At     int64  `json:"at"`
	Actor  string `json:"actor"`
	Action string `json:"action"`
	Object string `json:"object"`
}
type State struct {
	Version       int                `json:"version"`
	Users         map[string]User    `json:"users"`
	Accounts      map[string]Account `json:"accounts"`
	Jobs          map[string]Job     `json:"jobs"`
	CooldownUntil int64              `json:"cooldown_until"`
	Audit         []Audit            `json:"audit"`
}

var validID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
var protein = regexp.MustCompile(`^[ACDEFGHIKLMNPQRSTVWY]+$`)

func fail(code string) error { return errors.New(code) }
func stamp() int64           { return time.Now().UnixMilli() }
func ID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func Hash(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func Normalize(in Input) (Input, error) {
	in.Sequences = append([]Sequence(nil), in.Sequences...)
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		in.Name = time.Now().Format("2006-01-02_15:04")
	}
	if len([]rune(in.Name)) > 80 {
		return in, fail("INVALID_NAME")
	}
	if len(in.Sequences) == 0 {
		return in, fail("INVALID_SEQUENCES")
	}
	total := 0
	for i := range in.Sequences {
		s := &in.Sequences[i]
		s.Sequence = strings.Join(strings.Fields(s.Sequence), "")
		if !protein.MatchString(s.Sequence) {
			return in, fail("INVALID_SEQUENCE")
		}
		if s.Copies == 0 {
			s.Copies = 1
		}
		if s.Copies < 1 || s.Copies > 5000 || len(s.Sequence) > 5000 {
			return in, fail("INVALID_COPIES_OR_SIZE")
		}
		total += len(s.Sequence) * s.Copies
		if total > 5000 {
			return in, fail("INVALID_TOTAL_SIZE")
		}
	}
	return in, nil
}
func initial() State {
	return State{Version: 1, Users: map[string]User{}, Accounts: map[string]Account{}, Jobs: map[string]Job{}}
}
func (s *State) log(actor, action, object string) {
	s.Audit = append(s.Audit, Audit{stamp(), actor, action, object})
	if len(s.Audit) > 10000 {
		s.Audit = s.Audit[len(s.Audit)-10000:]
	}
}
