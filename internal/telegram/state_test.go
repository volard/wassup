package telegramsync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gotd/td/session"
)

func TestPrivateStateAndLock(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "telegram")
	release, err := lockState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := lockState(dir); err == nil {
		other()
		t.Fatal("concurrent lock accepted")
	}
	release()
	release, err = lockState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	path := filepath.Join(dir, "session.json")
	storage := sessionStorage{path: path}
	if _, err := storage.LoadSession(context.Background()); !errors.Is(err, session.ErrNotFound) {
		t.Fatal(err)
	}
	if err := storage.StoreSession(context.Background(), []byte("secret")); err != nil {
		t.Fatal(err)
	}
	if err := storage.StoreSession(context.Background(), []byte("replacement")); err != nil {
		t.Fatal(err)
	}
	got, err := storage.LoadSession(context.Background())
	if err != nil || string(got) != "replacement" {
		t.Fatal(string(got), err)
	}
	for path, mode := range map[string]os.FileMode{dir: 0700, storage.path: 0600} {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != mode {
			t.Fatalf("%s: %v", path, info.Mode())
		}
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.LoadSession(context.Background()); err == nil {
		t.Fatal("insecure session accepted")
	}
}
func TestLinksAccountAndDuplicateGuards(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "links.json")
	saved, err := loadLinks(path, 123)
	if err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(dir, "one.md")
	if err := saved.bind(note, Person{ID: 456}); err != nil {
		t.Fatal(err)
	}
	if err := saved.bind(filepath.Join(dir, "two.md"), Person{ID: 456}); err == nil {
		t.Fatal("duplicate accepted")
	}
	if err := writeJSON(path, saved); err != nil {
		t.Fatal(err)
	}
	if _, err := loadLinks(path, 999); err == nil {
		t.Fatal("wrong account accepted")
	}
	loaded, err := loadLinks(path, 123)
	if err != nil || loaded.Notes[note].ID != 456 {
		t.Fatal(loaded, err)
	}
	if err := os.WriteFile(path, []byte(`{"account_id":123,"notes":{}} {}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadLinks(path, 123); err == nil {
		t.Fatal("trailing JSON accepted")
	}
}
func TestStateRejectsSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	if release, err := lockState(alias); err == nil {
		release()
		t.Fatal("symlink directory accepted")
	}
	file := filepath.Join(target, "session")
	if err := os.WriteFile(file, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	aliasFile := filepath.Join(dir, "file")
	if err := os.Symlink(file, aliasFile); err != nil {
		t.Fatal(err)
	}
	if _, err := privateRead(aliasFile); err == nil {
		t.Fatal("symlink session accepted")
	}
}

func TestLinksRequireStoredAccountID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "links.json")
	if err := os.WriteFile(path, []byte(`{"notes":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadLinks(path, 123); err == nil {
		t.Fatal("missing account ID accepted")
	}
}
