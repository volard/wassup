package telegramsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gotd/td/session"
	"golang.org/x/sys/unix"
)

type credentials struct {
	AppID   int    `json:"api_id"`
	AppHash string `json:"api_hash"`
}

func (c credentials) validate() error {
	if c.AppID <= 0 || len(c.AppHash) != 32 || strings.IndexFunc(c.AppHash, func(r rune) bool { return !strings.ContainsRune("0123456789abcdefABCDEF", r) }) >= 0 {
		return errors.New("Telegram requires a positive api_id and a 32-character hexadecimal api_hash from my.telegram.org")
	}
	return nil
}

type Person struct {
	ID         int64  `json:"id"`
	AccessHash int64  `json:"access_hash"`
	Name       string `json:"name"`
	Username   string `json:"username,omitempty"`
	Phone      string `json:"phone,omitempty"`
}

type links struct {
	AccountID int64             `json:"account_id"`
	Notes     map[string]Person `json:"notes"`
}

func (l *links) bind(path string, person Person) error {
	for other, p := range l.Notes {
		if other != path && p.ID == person.ID {
			return fmt.Errorf("Telegram user %d is already linked to %s", p.ID, other)
		}
	}
	l.Notes[path] = person
	return nil
}

// Each config has its own account/session, including when multiple configs
// share a parent directory. None of these files belong in the notes directory.
func StateDir(configPath string) string { return configPath + ".telegram" }

func lockState(dir string) (func(), error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("Telegram state directory must be a real directory")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	fd, err := unix.Open(filepath.Join(dir, "lock"), unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		unix.Close(fd)
		return nil, errors.New("another Telegram command is running for this config")
	}
	return func() { _ = unix.Flock(fd, unix.LOCK_UN); _ = unix.Close(fd) }, nil
}

func readJSON(path string, value any) error {
	data, err := privateRead(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("invalid trailing data in %s", filepath.Base(path))
	}
	return nil
}

func privateRead(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s must be a regular file", filepath.Base(path))
	}
	if info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("%s must have private permissions (chmod 600)", path)
	}
	return os.ReadFile(path)
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return privateWrite(path, append(data, '\n'))
}

func privateWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".telegram-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

type sessionStorage struct{ path string }

func (s sessionStorage) LoadSession(context.Context) ([]byte, error) {
	data, err := privateRead(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, session.ErrNotFound
	}
	return data, err
}
func (s sessionStorage) StoreSession(_ context.Context, data []byte) error {
	return privateWrite(s.path, data)
}

func loadLinks(path string, accountID int64) (links, error) {
	var l links
	if err := readJSON(path, &l); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return links{AccountID: accountID, Notes: make(map[string]Person)}, nil
		}
		return links{}, err
	}
	if l.AccountID != accountID {
		return links{}, errors.New("saved links belong to a different Telegram account; use a separate Wassup config")
	}
	if l.Notes == nil {
		l.Notes = make(map[string]Person)
	}
	seen := map[int64]bool{}
	for path, p := range l.Notes {
		if !filepath.IsAbs(path) || p.ID <= 0 || seen[p.ID] {
			return links{}, errors.New("invalid or duplicate Telegram contact links")
		}
		seen[p.ID] = true
	}
	return l, nil
}
