package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"wassup/internal/contacts"
	telegramsync "wassup/internal/telegram"
)

type fakeTelegram struct {
	snapshot map[string]telegramsync.LinkStatus
	checked  map[string]telegramsync.LinkStatus
	checkErr error
	target   contacts.Contact
	value    string
	keepID   bool
	saves    int
}

func (f *fakeTelegram) Snapshot() (map[string]telegramsync.LinkStatus, error) {
	copy := map[string]telegramsync.LinkStatus{}
	for k, v := range f.snapshot {
		copy[k] = v
	}
	return copy, nil
}
func (f *fakeTelegram) Check(context.Context, []contacts.Contact) (map[string]telegramsync.LinkStatus, error) {
	return f.checked, f.checkErr
}
func (f *fakeTelegram) Resolve(_ context.Context, note contacts.Contact, value string, keepID bool) (telegramsync.Candidate, error) {
	f.target, f.value, f.keepID = note, value, keepID
	return telegramsync.Candidate{Person: telegramsync.Person{ID: 42, Name: "Telegram Alex", Username: "alex"}, AccountID: 1, Reference: "@alex"}, nil
}
func (f *fakeTelegram) Save(_ context.Context, note contacts.Contact, c telegramsync.Candidate) (telegramsync.LinkStatus, error) {
	f.target = note
	f.saves++
	return telegramsync.LinkStatus{Person: c.Person, State: telegramsync.LinkValid}, nil
}
func telegramModel(t *testing.T, metadata string) (Model, string, *fakeTelegram) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "alex.md")
	if err := os.WriteFile(path, []byte("---\ncnt:name: Alex\n"+metadata+"---\nNotes\n"), 0600); err != nil {
		t.Fatal(err)
	}
	service := &fakeTelegram{}
	m := New(dir, contacts.DefaultSchema(), "all", testOpener(), "").WithTelegram(service)
	return m, path, service
}
func dispatchTelegram(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected async Telegram command")
	}
	message := cmd()
	if batch, ok := message.(tea.BatchMsg); ok {
		message = batch[0]()
	}
	updated, _ := m.Update(message)
	return updated.(Model)
}
func TestTelegramInputUsesNoteUsernameOrPhone(t *testing.T) {
	for _, tc := range []struct{ metadata, want string }{{"cnt:telegram: '@alex'\ncnt:phone: '+1234567890'\n", "@alex"}, {"cnt:phone: '+1234567890'\n", "+1234567890"}, {"", ""}} {
		m, _, _ := telegramModel(t, tc.metadata)
		m = update(t, m, key("t"))
		if m.input != inputTelegram || m.inputValue != tc.want {
			t.Fatalf("input %q want %q", m.inputValue, tc.want)
		}
		m = update(t, m, key("ctrl+u"))
		for _, r := range "https://t.me/new_name" {
			m = update(t, m, runeKey(r))
		}
		if m.inputValue != "https://t.me/new_name" {
			t.Fatal(m.inputValue)
		}
	}
}
func TestTelegramResolveConfirmAndSaveSelectedContact(t *testing.T) {
	m, path, service := telegramModel(t, "cnt:telegram: '@alex'\n")
	m = update(t, m, key("t"))
	updated, cmd := m.Update(key("enter"))
	m = updated.(Model)
	if !m.telegramBusy || m.input != inputTelegramWaiting {
		t.Fatal("not waiting asynchronously")
	}
	m = dispatchTelegram(t, m, cmd)
	if m.input != inputTelegramConfirm || service.saves != 0 {
		t.Fatal("saved before confirmation")
	}
	if service.target.Path != path || service.value != "@alex" || service.keepID {
		t.Fatal("wrong lookup")
	}
	updated, cmd = m.Update(key("enter"))
	m = dispatchTelegram(t, updated.(Model), cmd)
	if service.saves != 1 || service.target.Path != path || m.input != inputNone {
		t.Fatal("wrong save")
	}
	if !strings.Contains(m.row(m.contacts[0], true, 80), "✈✓") {
		t.Fatal("verified icon missing")
	}
}
func TestExistingLinkKeepsStableIDWhenDetailsChange(t *testing.T) {
	m, path, service := telegramModel(t, "cnt:telegram: '@old_name'\n")
	m.telegramLinks[path] = telegramsync.LinkStatus{Person: telegramsync.Person{ID: 42, Username: "new_name"}, State: telegramsync.LinkChanged}
	m = update(t, m, key("t"))
	if m.inputValue != "@new_name" {
		t.Fatal(m.inputValue)
	}
	updated, cmd := m.Update(key("enter"))
	m = dispatchTelegram(t, updated.(Model), cmd)
	if !service.keepID {
		t.Fatal("unchanged input re-resolved mutable username")
	}
	m = update(t, m, key("esc"))
	m = update(t, m, key("t"))
	m = update(t, m, key("ctrl+u"))
	for _, r := range "@different" {
		m = update(t, m, runeKey(r))
	}
	updated, cmd = m.Update(key("enter"))
	_ = dispatchTelegram(t, updated.(Model), cmd)
	if service.keepID {
		t.Fatal("explicit replacement did not resolve new identity")
	}
}
func TestCanceledLookupIgnoresLateResult(t *testing.T) {
	m, _, service := telegramModel(t, "cnt:telegram: '@alex'\n")
	m = update(t, m, key("t"))
	updated, cmd := m.Update(key("enter"))
	m = updated.(Model)
	m = update(t, m, key("esc"))
	m = dispatchTelegram(t, m, cmd)
	if m.input != inputNone || service.saves != 0 || m.telegramBusy {
		t.Fatal("late reply reopened or saved contact")
	}
}
func TestValidationFailureRemainsUnknownAndDoesNotBlockNavigation(t *testing.T) {
	m, path, service := telegramModel(t, "")
	service.snapshot = map[string]telegramsync.LinkStatus{path: {Person: telegramsync.Person{ID: 42}, State: telegramsync.LinkUnknown}}
	service.checkErr = errors.New("offline")
	m = m.WithTelegram(service)
	if m.telegramLinks[path].State != telegramsync.LinkUnknown {
		t.Fatal("cached link treated as verified")
	}
	m = update(t, m, key("tab"))
	m = dispatchTelegram(t, m, m.Init())
	if m.telegramLinks[path].State != telegramsync.LinkUnknown || !m.statusErr {
		t.Fatal("network failure classified as invalid")
	}
}
func TestTelegramBadgesAlignAndShowDistinctStates(t *testing.T) {
	m, path, _ := telegramModel(t, "")
	width := -1
	for state, icon := range map[telegramsync.LinkState]string{telegramsync.LinkUnknown: "✈?", telegramsync.LinkValid: "✈✓", telegramsync.LinkChanged: "✈~", telegramsync.LinkInvalid: "✈!"} {
		m.telegramLinks[path] = telegramsync.LinkStatus{Person: telegramsync.Person{ID: 42}, State: state}
		badge := m.telegramBadge(path)
		if !strings.Contains(badge, icon) {
			t.Fatal(badge)
		}
		if width < 0 {
			width = lipgloss.Width(badge)
		} else if lipgloss.Width(badge) != width {
			t.Fatal("badge widths differ")
		}
	}
	if lipgloss.Width(m.telegramBadge("unlinked")) != width {
		t.Fatal("unlinked spacing differs")
	}
}

func TestTelegramFooterFitsSmallTerminal(t *testing.T) {
	m, _, _ := telegramModel(t, "")
	base := m.contacts[0]
	for i := 0; i < 50; i++ {
		m.contacts = append(m.contacts, base)
	}
	m.width, m.height = 80, 24
	m.telegramLinks[base.Path] = telegramsync.LinkStatus{Person: telegramsync.Person{ID: 42, Username: "alex"}, State: telegramsync.LinkValid, Detail: "Telegram account verified"}
	for _, input := range []inputMode{inputNone, inputTelegram, inputTelegramConfirm} {
		m.input = input
		m.telegramTarget = base
		m.inputValue = "@alex"
		m.telegramCandidate = telegramsync.Candidate{Person: telegramsync.Person{Name: "Alex", ID: 42}, Reference: "@alex"}
		lines := 0
		for _, line := range strings.Split(m.View(), "\n") {
			lines += max(1, (lipgloss.Width(line)+79)/80)
		}
		if lines > 24 {
			t.Fatalf("input %d renders %d lines in 24-row terminal", input, lines)
		}
	}
}

func TestScheduledChecksSkipFreshCacheAndManualChecksOverride(t *testing.T) {
	m, path, service := telegramModel(t, "")
	now := time.Now()
	service.snapshot = map[string]telegramsync.LinkStatus{path: {Person: telegramsync.Person{ID: 42}, State: telegramsync.LinkValid, CheckedAt: now, LastAttemptAt: now}}
	m = m.WithTelegram(service, 14)
	if m.Init() != nil || m.telegramBusy {
		t.Fatal("fresh cache triggered startup network request")
	}
	updated, cmd := m.Update(key("r"))
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("reload forced a fresh check")
	}
	updated, cmd = m.Update(key("V"))
	m = updated.(Model)
	if cmd == nil || !m.telegramBusy || m.telegramTotal != 1 {
		t.Fatal("manual all check not started")
	}
	m = update(t, m, key("esc"))
	if m.telegramBusy {
		t.Fatal("escape did not cancel background check")
	}
	m = m.WithTelegram(service, 0)
	if m.Init() != nil || m.telegramBusy {
		t.Fatal("manual-only started automatic check")
	}
	_, cmd = m.Update(key("v"))
	if cmd == nil {
		t.Fatal("manual selected check disabled")
	}
}
func TestProgressShowsPhaseCountElapsedAndStopsAfterTimeout(t *testing.T) {
	m, path, service := telegramModel(t, "")
	service.snapshot = map[string]telegramsync.LinkStatus{path: {Person: telegramsync.Person{ID: 42}, State: telegramsync.LinkUnknown}}
	service.checkErr = context.DeadlineExceeded
	m = m.WithTelegram(service, 0)
	updated, command := m.Update(key("V"))
	m = updated.(Model)
	m.telegramProgress <- telegramsync.Progress{Phase: "Checking accounts", Done: 3, Total: 13}
	updated, next := m.Update(telegramTick{request: m.telegramRequest, now: m.telegramStarted.Add(5 * time.Second)})
	m = updated.(Model)
	if next == nil || !strings.Contains(m.footer(), "3/13 contacts") || !strings.Contains(m.footer(), "Checking accounts") || !strings.Contains(m.footer(), "5s") {
		t.Fatal(m.footer())
	}
	m = dispatchTelegram(t, m, command)
	if m.telegramBusy || !m.statusErr || !strings.Contains(m.status, "timed out") || strings.Contains(m.status, "deadline exceeded") {
		t.Fatal(m.status)
	}
	if m.telegramLinks[path].Person.ID != 42 {
		t.Fatal("lost cached link")
	}
	_, next = m.Update(telegramTick{request: m.telegramRequest, now: time.Now()})
	if next != nil {
		t.Fatal("spinner still scheduled after failure")
	}
}
