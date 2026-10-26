package main

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"github.com/bl0omz/keepquiet/internal/redact"
)

// store is the placeholder vault shared by every hook process:
// ~/.keepquiet/{redact.key, vault.json, vault.lock}. vault.json holds
// the real values in plain text (mode 0600), as magpie holds them in the
// gateway's memory; see README.
type store struct {
	dir  string
	lock *os.File
	// saved is redact.Count() when the vault file last matched what this
	// process holds (after load or save): with nothing new since, save
	// has nothing to write.
	saved int
}

func openStore() (*store, error) {
	dir := homeDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &store{dir: dir}
	if err := s.acquire(); err != nil {
		return nil, err
	}
	if err := s.ensureKey(); err != nil {
		s.release()
		return nil, err
	}
	redact.SetKeyPath(filepath.Join(dir, "redact.key"))
	if err := s.load(); err != nil {
		s.release()
		return nil, err
	}
	s.saved = redact.Count()
	s.release()
	return s, nil
}

func (s *store) acquire() error {
	f, err := os.OpenFile(filepath.Join(s.dir, "vault.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return err
	}
	s.lock = f
	return nil
}

func (s *store) release() {
	if s.lock != nil {
		_ = syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN)
		s.lock.Close()
		s.lock = nil
	}
}

// ensureKey makes the HMAC key once, so concurrent first runs agree on it
// (redact.loadKey alone would race).
func (s *store) ensureKey() error {
	p := filepath.Join(s.dir, "redact.key")
	if b, err := os.ReadFile(p); err == nil && len(b) >= 32 {
		return nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

func (s *store) vaultPath() string { return filepath.Join(s.dir, "vault.json") }

// load imports the vault; called with the lock held.
func (s *store) load() error {
	b, err := os.ReadFile(s.vaultPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		return errors.New("vault.json is corrupt: " + err.Error())
	}
	redact.Import(m)
	return nil
}

// dirty says this process holds placeholders the vault file may lack.
func (s *store) dirty() bool { return redact.Count() != s.saved }

// save merges what other processes saved since, then writes everything
// this process knows.
func (s *store) save() error {
	if !s.dirty() {
		return nil
	}
	if err := s.acquire(); err != nil {
		return err
	}
	defer s.release()
	if err := s.load(); err != nil {
		return err
	}
	b, err := json.Marshal(redact.Export())
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, "vault-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.vaultPath()); err != nil {
		return err
	}
	s.saved = redact.Count()
	return nil
}

func (s *store) clear() error {
	if err := s.acquire(); err != nil {
		return err
	}
	defer s.release()
	err := os.Remove(s.vaultPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
