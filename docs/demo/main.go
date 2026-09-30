package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"wassup/internal/contacts"
	telegramsync "wassup/internal/telegram"
	"wassup/internal/ui"
)

type demoTelegram struct {
	path   string
	person telegramsync.Person
}

func (d demoTelegram) Snapshot() (map[string]telegramsync.LinkStatus, error) {
	return map[string]telegramsync.LinkStatus{
		d.path: {Person: d.person, State: telegramsync.LinkUnknown, Detail: "Demo account; press v to verify"},
	}, nil
}

func (d demoTelegram) Check(ctx context.Context, notes []contacts.Contact) (map[string]telegramsync.LinkStatus, error) {
	telegramsync.ReportProgress(ctx, telegramsync.Progress{Phase: "Checking demo account", Total: len(notes)})
	select {
	case <-time.After(time.Second):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	now := time.Now()
	statuses := make(map[string]telegramsync.LinkStatus, len(notes))
	for _, note := range notes {
		if note.Path == d.path {
			if _, err := contacts.ImportTelegram(note.Path, "", d.person.Phone, time.Time{}, contacts.DefaultSchema(), false); err != nil {
				return nil, err
			}
			statuses[note.Path] = telegramsync.LinkStatus{
				Person: d.person, State: telegramsync.LinkValid, Detail: "Demo account verified",
				CheckedAt: now, LastAttemptAt: now,
			}
		}
	}
	return statuses, nil
}

func (demoTelegram) Resolve(context.Context, contacts.Contact, string, bool) (telegramsync.Candidate, error) {
	return telegramsync.Candidate{}, errors.New("Telegram linking is not simulated in the demo")
}

func (demoTelegram) Save(context.Context, contacts.Contact, telegramsync.Candidate) (telegramsync.LinkStatus, error) {
	return telegramsync.LinkStatus{}, errors.New("Telegram linking is not simulated in the demo")
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: wassup-demo <contacts-directory>")
		os.Exit(2)
	}
	dir, err := filepath.Abs(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	service := demoTelegram{
		path:   filepath.Join(dir, "maya-chen.md"),
		person: telegramsync.Person{ID: 4242, Name: "Maya Chen", Username: "maya_demo", Phone: "+15550104242"},
	}
	model := ui.New(dir, contacts.DefaultSchema(), "keep_in_touch", []string{"nvim", "{path}"}, "Demo contacts · Telegram check is simulated").WithTelegram(service, 0)
	if _, err := tea.NewProgram(model, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
