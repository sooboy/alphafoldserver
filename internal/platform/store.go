package platform

import (
	"encoding/json"
	"github.com/gofrs/flock"
	"os"
	"path/filepath"
	"sync"
)

type Store struct {
	mu    sync.Mutex
	Dir   string
	state State
	lock  *flock.Flock
}

func OpenStore(dir string) (*Store, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	lock := flock.New(filepath.Join(dir, "platform.lock"))
	ok, err := lock.TryLock()
	if err != nil || !ok {
		return nil, fail("PLATFORM_DATA_LOCKED")
	}
	s := &Store{Dir: dir, lock: lock, state: initial()}
	b, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err == nil {
		err = json.Unmarshal(b, &s.state)
	} else if os.IsNotExist(err) {
		err = nil
	}
	if err != nil {
		lock.Unlock()
		return nil, err
	}
	if s.state.Version != 1 || s.state.Users == nil || s.state.Accounts == nil || s.state.Jobs == nil {
		lock.Unlock()
		return nil, fail("INVALID_STATE")
	}
	err = s.Update(func(st *State) error {
		for id, j := range st.Jobs {
			switch j.Stage {
			case "creating", "submitting", "draft":
				j.Stage = "submission_unknown"
				j.Error = "RESTART_DURING_REMOTE_WRITE"
			case "preflight":
				j.Stage = "queued"
				j.AccountID = ""
			}
			st.Jobs[id] = j
		}
		return nil
	})
	if err != nil {
		lock.Unlock()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() error { return s.lock.Unlock() }
func copyState(in State) State {
	b, _ := json.Marshal(in)
	var out State
	_ = json.Unmarshal(b, &out)
	return out
}
func (s *Store) Snapshot() State { s.mu.Lock(); defer s.mu.Unlock(); return copyState(s.state) }
func (s *Store) Update(fn func(*State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := copyState(s.state)
	if err := fn(&next); err != nil {
		return err
	}
	b, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if err = atomicFile(filepath.Join(s.Dir, "state.json"), append(b, '\n')); err != nil {
		return err
	}
	s.state = next
	return nil
}
func atomicFile(name string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(name), ".write-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp, name)
}
