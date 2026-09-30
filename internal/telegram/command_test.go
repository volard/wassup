package telegramsync

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wassup/internal/contacts"
)

func TestCommandParsing(t *testing.T) {
	for _, args := range [][]string{{"sync", "--dry-run"}, {"login"}, {"link", "--note", "Alex.md", "--user", "@alex"}} {
		if _, err := parseCommand(args, &bytes.Buffer{}); err != nil {
			t.Fatal(args, err)
		}
	}
	for _, args := range [][]string{{"unknown"}, {"sync", "--user", "alex"}, {"sync", "extra"}, {"link", "--user", "alex"}} {
		if _, err := parseCommand(args, &bytes.Buffer{}); err == nil {
			t.Fatal(args)
		}
	}
	for _, args := range [][]string{nil, {"help"}, {"sync", "--help"}} {
		if _, err := parseCommand(args, &bytes.Buffer{}); !errors.Is(err, flag.ErrHelp) {
			t.Fatal(args, err)
		}
	}
}
func TestHelpAndInvalidCommandsDoNotCreateState(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "config.json")
	for _, args := range [][]string{{"help"}, {"sync", "--bad-flag"}} {
		_ = Run(context.Background(), args, config, dir, contacts.DefaultSchema(), nil, &bytes.Buffer{}, &bytes.Buffer{})
		if _, err := os.Stat(StateDir(config)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("created state", err)
		}
	}
}
func TestSyncWithoutCredentialsDoesNotPrompt(t *testing.T) {
	dir := t.TempDir()
	fixture(t, dir, "Alex", "")
	config := filepath.Join(t.TempDir(), "config.json")
	var out bytes.Buffer
	err := Run(context.Background(), []string{"sync", "--dry-run"}, config, dir, contacts.DefaultSchema(), nil, &out, &out)
	if err == nil || !strings.Contains(err.Error(), "login") {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "api_id") {
		t.Fatal("prompted during sync")
	}
}

type fakePeople struct{ people []Person }

func (f fakePeople) People(context.Context) ([]Person, error) { return f.people, nil }
func (f fakePeople) Resolve(_ context.Context, handle string) (Person, error) {
	for _, p := range f.people {
		if username(handle) == p.Username {
			return p, nil
		}
	}
	return Person{}, errors.New("not found")
}
func TestInteractiveLinkRequiresConfirmationAndPersistsID(t *testing.T) {
	for _, accept := range []bool{false, true} {
		t.Run(map[bool]string{false: "decline", true: "accept"}[accept], func(t *testing.T) {
			dir := t.TempDir()
			note := fixture(t, dir, "Alex", "")
			before, _ := os.ReadFile(note)
			path := filepath.Join(dir, "links.json")
			answer := "@alex\nn\n"
			if accept {
				answer = "/alex\n42\ny\n"
			}
			var out bytes.Buffer
			prompt := &terminalPrompt{reader: bufio.NewReader(strings.NewReader(answer)), out: &out}
			saved := links{AccountID: 7, Notes: map[string]Person{}}
			err := linkNotes(context.Background(), fakePeople{[]Person{{ID: 42, Name: "Alex", Username: "alex", AccessHash: 99}}}, []contacts.Contact{{Path: note, Name: "Alex"}}, &saved, path, commandOptions{}, prompt, &out)
			if err != nil {
				t.Fatal(err)
			}
			if accept {
				loaded, err := loadLinks(path, 7)
				if err != nil || loaded.Notes[note].ID != 42 {
					t.Fatal(loaded, err)
				}
			} else {
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("saved declined link")
				}
			}
			after, _ := os.ReadFile(note)
			if !bytes.Equal(before, after) {
				t.Fatal("link changed note")
			}
		})
	}
}

func TestPromptCancellation(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	prompt := &terminalPrompt{in: read, ctx: ctx}
	if _, err := (promptReader{prompt}).Read(make([]byte, 1)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
