package ui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"wassup/internal/contacts"
)

func TestScheduleContactThroughUI(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "person.md")
	if err := os.WriteFile(path, []byte("---\ncnt:name: Person\n---\n# Person\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	model := New(dir, contacts.DefaultSchema(), "all", testOpener(), "")
	if model.view != viewAll {
		t.Fatalf("initial view = %d, want All", model.view)
	}
	model = update(t, model, key("a"))
	model = update(t, model, runeKey('3'))
	model = update(t, model, runeKey('0'))
	model = update(t, model, key("enter"))

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "cnt:contact-every-days: 30\n---") {
		t.Fatalf("schedule not persisted:\n%s", data)
	}
	if model.input != inputNone || model.statusErr {
		t.Fatalf("unexpected UI state: input=%d status=%q", model.input, model.status)
	}
}

func TestContactDateCanBeHistoricalOrDefaultToToday(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "person.md")
	content := "---\ncnt:name: Person\ncnt:contact-every-days: 30\n---\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	model := New(dir, contacts.DefaultSchema(), "all", testOpener(), "")
	model.today = time.Date(2026, time.August, 29, 0, 0, 0, 0, time.Local)

	model = update(t, model, key("d"))
	if model.input != inputContactDate {
		t.Fatalf("d input mode = %d, want contact date", model.input)
	}
	for _, r := range "2026-06-15" {
		model = update(t, model, runeKey(r))
	}
	model = update(t, model, key("enter"))
	if got := readFile(t, path); !strings.Contains(got, "cnt:last-contact: 2026-06-15") {
		t.Fatalf("historical contact date not persisted:\n%s", got)
	}

	model = update(t, model, key("d"))
	model = update(t, model, key("enter"))
	if got := readFile(t, path); !strings.Contains(got, "cnt:last-contact: 2026-08-29") {
		t.Fatalf("blank contact date did not default to today:\n%s", got)
	}
}

func TestContactDateRejectsFutureDate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "person.md")
	content := "---\ncnt:name: Person\ncnt:contact-every-days: 30\n---\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	model := New(dir, contacts.DefaultSchema(), "all", testOpener(), "")
	model.today = time.Date(2026, time.August, 29, 0, 0, 0, 0, time.Local)
	model = update(t, model, key("d"))
	for _, r := range "2026-08-30" {
		model = update(t, model, runeKey(r))
	}
	model = update(t, model, key("enter"))
	if !model.statusErr || model.input != inputContactDate || !strings.Contains(model.status, "future") {
		t.Fatalf("future date state: input=%d status=%q error=%v", model.input, model.status, model.statusErr)
	}
	if got := readFile(t, path); strings.Contains(got, "cnt:last-contact") {
		t.Fatalf("future contact date was persisted:\n%s", got)
	}
}

func TestSearchAcceptsCyrillicRunes(t *testing.T) {
	model := Model{input: inputSearch}
	model = update(t, model, runeKey('Ю'))
	model = update(t, model, runeKey('л'))
	model = update(t, model, runeKey('я'))
	if model.query != "Юля" {
		t.Fatalf("query = %q", model.query)
	}
}

func TestPreviewUsesRightPaneOnlyOnWideTerminals(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "person.md")
	content := "---\ncnt:name: Person\ncnt:birth-date: '--03-24'\nrel:origin: school\n---\n# Person\n\nA useful preview.\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	model := New(dir, contacts.DefaultSchema(), "all", testOpener(), "")
	model.preview = true
	model.width, model.height = 120, 30
	if !lineContainsBoth(model.View(), "Person", "╭") {
		t.Fatal("wide preview was not rendered beside the contact list")
	}
	for _, expected := range []string{"cnt:birth-date: --03-24", "rel:origin: school", "A useful preview."} {
		if !strings.Contains(model.View(), expected) {
			t.Fatalf("wide preview does not contain %q", expected)
		}
	}

	model.width = 80
	if lineContainsBoth(model.View(), "Person", "╭") {
		t.Fatal("narrow preview should be stacked below the contact list")
	}
	if !strings.Contains(model.View(), "rel:origin: school") {
		t.Fatal("narrow preview does not contain frontmatter")
	}
}

func TestKeepInTouchIsSeparateFromAllAndCanRemoveContact(t *testing.T) {
	dir := t.TempDir()
	trackedPath := filepath.Join(dir, "tracked.md")
	tracked := "---\ncnt:name: Tracked\ncnt:contact-every-days: 30\n---\n# Tracked\n"
	if err := os.WriteFile(trackedPath, []byte(tracked), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "information.md"), []byte("---\ncnt:name: Information\n---\n# Information\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	model := New(dir, contacts.DefaultSchema(), "keep_in_touch", testOpener(), "")
	if model.view != viewKeepInTouch || len(model.visible()) != 1 || model.visible()[0].Name != "Tracked" {
		t.Fatalf("Keep in touch contents = %#v", model.visible())
	}
	if model.count(viewAll) != 2 {
		t.Fatalf("All count = %d, want 2", model.count(viewAll))
	}
	model = update(t, model, key("x"))
	if len(model.visible()) != 0 || model.count(viewAll) != 2 {
		t.Fatalf("after removal: keep=%d all=%d", len(model.visible()), model.count(viewAll))
	}
	data, err := os.ReadFile(trackedPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "cnt:contact-every-days") {
		t.Fatalf("schedule field was not removed:\n%s", data)
	}
}

func TestConfiguredStartView(t *testing.T) {
	for name, want := range map[string]viewMode{
		"due":           viewDue,
		"upcoming":      viewUpcoming,
		"keep_in_touch": viewKeepInTouch,
		"all":           viewAll,
	} {
		model := New(t.TempDir(), contacts.DefaultSchema(), name, testOpener(), "")
		if model.view != want {
			t.Errorf("start view %q = %d, want %d", name, model.view, want)
		}
	}
}

func TestPreviewIsOpenByDefault(t *testing.T) {
	model := New(t.TempDir(), contacts.DefaultSchema(), "all", testOpener(), "")
	if !model.preview {
		t.Fatal("preview is closed in the initial model")
	}
	model = update(t, model, key("tab"))
	if !model.preview {
		t.Fatal("changing views closed the default preview")
	}
}

func TestBracketKeysNavigateViewsBothWays(t *testing.T) {
	model := New(t.TempDir(), contacts.DefaultSchema(), "due", testOpener(), "")
	model = update(t, model, key("]"))
	if model.view != viewUpcoming {
		t.Fatalf("] selected view %d, want Upcoming", model.view)
	}
	model = update(t, model, key("["))
	if model.view != viewDue {
		t.Fatalf("[ selected view %d, want Due", model.view)
	}
	model = update(t, model, key("["))
	if model.view != viewAll {
		t.Fatalf("[ did not wrap backward: view=%d", model.view)
	}
}

func TestRowsAlignDateColumnForLatinAndCyrillicNames(t *testing.T) {
	model := Model{today: time.Date(2026, time.August, 29, 0, 0, 0, 0, time.Local)}
	lastContact := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.Local)
	contactsToRender := []contacts.Contact{
		{Name: "Aleksandr Sokolov", Tracked: true, LastContact: &lastContact, EveryDays: 30},
		{Name: "Александр Соколов", Tracked: true, LastContact: &lastContact, EveryDays: 30},
		{Name: "👪 Семья", Tracked: true, LastContact: &lastContact, EveryDays: 30},
	}
	wantColumn := -1
	for _, contact := range contactsToRender {
		row := model.row(contact, false, 80)
		date := contact.DueDate(model.today).Format("02 Jan 2006")
		dateAt := strings.Index(row, date)
		if dateAt < 0 {
			t.Fatalf("date missing from row %q", row)
		}
		column := lipgloss.Width(row[:dateAt])
		if wantColumn < 0 {
			wantColumn = column
		} else if column != wantColumn {
			t.Fatalf("%q date column = %d, want %d", contact.Name, column, wantColumn)
		}
	}
}

func TestTruncateUsesTerminalCellWidth(t *testing.T) {
	for _, test := range []struct {
		value string
		width int
	}{
		{value: "Александр", width: 6},
		{value: "👪 Семья", width: 6},
	} {
		got := truncate(test.value, test.width)
		if width := lipgloss.Width(got); width > test.width {
			t.Fatalf("truncate(%q, %d) = %q with width %d", test.value, test.width, got, width)
		}
	}
}

func TestOpenCommandSubstitutesPathWithoutShellParsing(t *testing.T) {
	path := "/tmp/contact with spaces.md"
	command, err := openCommand([]string{"nvim", "--", "{path}"}, path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"nvim", "--", path}
	if !reflect.DeepEqual(command.Args, want) {
		t.Fatalf("arguments = %#v, want %#v", command.Args, want)
	}
	if _, err := openCommand([]string{"nvim"}, path); err == nil {
		t.Fatal("expected missing placeholder error")
	}
}

func TestOpenKeyReturnsExternalProcessCommand(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "person.md"), []byte("---\ncnt:name: Person\n---\n# Person\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	model := New(dir, contacts.DefaultSchema(), "all", []string{"true", "{path}"}, "")
	_, command := model.Update(key("o"))
	if command == nil {
		t.Fatal("open key did not return an external process command")
	}
}

func lineContainsBoth(value, first, second string) bool {
	for line := range strings.Lines(value) {
		if strings.Contains(line, first) && strings.Contains(line, second) {
			return true
		}
	}
	return false
}

func update(t *testing.T, model Model, msg tea.KeyMsg) Model {
	t.Helper()
	updated, _ := model.Update(msg)
	result, ok := updated.(Model)
	if !ok {
		t.Fatalf("model type = %T", updated)
	}
	return result
}

func key(value string) tea.KeyMsg {
	switch value {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)}
	}
}

func runeKey(value rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{value}}
}

func testOpener() []string {
	return []string{"nvim", "{path}"}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
