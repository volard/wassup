package contacts

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScanReadsOnlyScheduledContacts(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "tracked.md"), `---
cnt:name: 'Иван Иванов'
cnt:birth-date: '--03-24'
rel:origin: school
cnt:last-contact: 2026-01-10
cnt:contact-every-days: 90
cnt:snooze-until: 2026-05-01
---
# Иван Иванов

Likes tea.
`)
	writeTestFile(t, filepath.Join(dir, "ordinary.md"), `---
cnt:name: Ordinary
---
# Ordinary
`)
	if err := os.Mkdir(filepath.Join(dir, "pictures"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, warnings := Scan(dir, DefaultSchema())
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if len(got) != 2 {
		t.Fatalf("got %d contacts, want 2", len(got))
	}
	var tracked Contact
	for _, contact := range got {
		if contact.Tracked {
			tracked = contact
		}
	}
	if tracked.Name != "Иван Иванов" {
		t.Errorf("name = %q", tracked.Name)
	}
	if tracked.EveryDays != 90 {
		t.Errorf("interval = %d", tracked.EveryDays)
	}
	if !strings.Contains(tracked.Note, "Likes tea.") {
		t.Errorf("note body was not retained: %q", tracked.Note)
	}
	for _, metadata := range []string{"cnt:name: Иван Иванов", "cnt:birth-date: --03-24", "rel:origin: school"} {
		if !strings.Contains(tracked.Note, metadata) {
			t.Errorf("preview does not contain %q: %q", metadata, tracked.Note)
		}
	}
	for _, scheduling := range []string{"cnt:last-contact", "cnt:contact-every-days", "cnt:snooze-until"} {
		if strings.Contains(tracked.Note, scheduling) {
			t.Errorf("preview contains scheduling field %q: %q", scheduling, tracked.Note)
		}
	}
	due := tracked.DueDate(date(t, 2026, time.April, 20))
	if want := "2026-05-01"; due.Format(dateLayout) != want {
		t.Errorf("due = %s, want %s", due.Format(dateLayout), want)
	}
}

func TestScanReportsInvalidScheduleWithoutBlockingValidNotes(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "bad.md"), "---\ncnt:contact-every-days: soon\n---\n# Bad\n")
	writeTestFile(t, filepath.Join(dir, "good.md"), "---\ncnt:contact-every-days: 30\n---\n# Good\n")

	got, warnings := Scan(dir, DefaultSchema())
	if len(got) != 1 || got[0].Name != "Good" {
		t.Fatalf("valid contacts = %#v", got)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0].Error(), "positive integer") {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestMarkContactedPreservesUnrelatedContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "person.md")
	original := `---
cnt:name: 'Person'

custom: "keep exactly"
cnt:last-contact: 2025-12-01
cnt:contact-every-days: 30
cnt:snooze-until: 2026-09-01
---
# Person

Unrelated body.
`
	writeTestFile(t, path, original)

	if err := MarkContacted(path, date(t, 2026, time.August, 28), DefaultSchema()); err != nil {
		t.Fatal(err)
	}
	got := readTestFile(t, path)
	want := `---
cnt:name: 'Person'

custom: "keep exactly"
cnt:last-contact: 2026-08-28
cnt:contact-every-days: 30
---
# Person

Unrelated body.
`
	if got != want {
		t.Fatalf("file changed unexpectedly\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestMarkContactedAddsMissingFieldAndPreservesCRLF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "person.md")
	original := "---\r\ncnt:name: Person\r\ncnt:contact-every-days: 30\r\n---\r\n# Person\r\n"
	writeTestFile(t, path, original)

	if err := MarkContacted(path, date(t, 2026, time.August, 28), DefaultSchema()); err != nil {
		t.Fatal(err)
	}
	got := readTestFile(t, path)
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Fatalf("line endings changed: %q", got)
	}
	if !strings.Contains(got, "cnt:last-contact: 2026-08-28\r\n---") {
		t.Fatalf("missing inserted field: %q", got)
	}
}

func TestSnoozeValidationAndUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "person.md")
	writeTestFile(t, path, "---\ncnt:contact-every-days: 30\n---\n# Person\n")
	if err := Snooze(path, date(t, 2026, time.August, 28), 0, DefaultSchema()); err == nil {
		t.Fatal("expected validation error")
	}
	if err := Snooze(path, date(t, 2026, time.August, 28), 14, DefaultSchema()); err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, path); !strings.Contains(got, "cnt:snooze-until: 2026-09-11") {
		t.Fatalf("snooze not written: %s", got)
	}
}

func TestScheduleAddsInterval(t *testing.T) {
	path := filepath.Join(t.TempDir(), "person.md")
	writeTestFile(t, path, "---\ncnt:name: Person\n---\n# Person\n")
	if err := Schedule(path, 90, DefaultSchema()); err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, path); !strings.Contains(got, "cnt:contact-every-days: 90\n---") {
		t.Fatalf("interval not written: %s", got)
	}
}

func TestRemoveFromKeepInTouchPreservesLastContact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "person.md")
	writeTestFile(t, path, "---\ncnt:last-contact: 2026-08-01\ncnt:contact-every-days: 30\ncnt:snooze-until: 2026-09-01\n---\n# Person\n")
	if err := RemoveFromKeepInTouch(path, DefaultSchema()); err != nil {
		t.Fatal(err)
	}
	got := readTestFile(t, path)
	if !strings.Contains(got, "cnt:last-contact: 2026-08-01") {
		t.Fatalf("last contact was removed:\n%s", got)
	}
	if strings.Contains(got, "cnt:contact-every-days") || strings.Contains(got, "cnt:snooze-until") {
		t.Fatalf("scheduling fields remain:\n%s", got)
	}
}

func TestDueDateUsesTodayWhenNeverContacted(t *testing.T) {
	today := date(t, 2026, time.August, 28)
	contact := Contact{EveryDays: 90}
	if got := contact.DueDate(today); !got.Equal(today) {
		t.Fatalf("due = %s, want %s", got, today)
	}
}

func TestCustomSchemaControlsReadsAndWrites(t *testing.T) {
	schema := DefaultSchema()
	schema.DisplayName = "person-name"
	schema.FallbackName = "person-alias"
	schema.LastContact = "last-seen"
	schema.ContactEveryDays = "contact-after-days"
	schema.SnoozeUntil = "not-before"
	dir := t.TempDir()
	path := filepath.Join(dir, "person.md")
	writeTestFile(t, path, `---
person-name: 'Иван'
last-seen: 2026-01-01
contact-after-days: 30
not-before: 2026-08-30
---
# Ignored heading
`)

	got, warnings := Scan(dir, schema)
	if len(warnings) != 0 || len(got) != 1 {
		t.Fatalf("contacts=%v warnings=%v", got, warnings)
	}
	if got[0].Name != "Иван" || !got[0].Tracked || got[0].EveryDays != 30 {
		t.Fatalf("custom fields were not read: %#v", got[0])
	}
	if err := MarkContacted(path, date(t, 2026, time.August, 28), schema); err != nil {
		t.Fatal(err)
	}
	if err := Schedule(path, 60, schema); err != nil {
		t.Fatal(err)
	}
	updated := readTestFile(t, path)
	for _, expected := range []string{"last-seen: 2026-08-28", "contact-after-days: 60"} {
		if !strings.Contains(updated, expected) {
			t.Errorf("missing %q in:\n%s", expected, updated)
		}
	}
	if strings.Contains(updated, "not-before:") || strings.Contains(updated, "cnt:") {
		t.Fatalf("wrong fields were written:\n%s", updated)
	}
}

func TestScanValidatesContactMetadata(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "valid.md"), "---\ncnt:name: Valid\ncnt:birth-date: '--03-24'\nrel:origin: school\n---\n")
	writeTestFile(t, filepath.Join(dir, "bad-date.md"), "---\ncnt:name: Bad date\ncnt:birth-date: 24/03/2024\n---\n")
	writeTestFile(t, filepath.Join(dir, "bad-origin.md"), "---\ncnt:name: Bad origin\nrel:origin: School\n---\n")
	writeTestFile(t, filepath.Join(dir, "unknown.md"), "---\ncnt:name: Unknown\nrel:early-study: true\n---\n")
	writeTestFile(t, filepath.Join(dir, "duplicate.md"), "---\ncnt:name: Duplicate\ncnt:name: Again\n---\n")

	got, warnings := Scan(dir, DefaultSchema())
	if len(got) != 1 || got[0].Name != "Valid" {
		t.Fatalf("valid contacts = %#v", got)
	}
	if len(warnings) != 4 {
		t.Fatalf("warnings = %v", warnings)
	}
	combined := fmt.Sprint(warnings)
	for _, expected := range []string{"YYYY-MM-DD or --MM-DD", "must be one of", "unknown contact field", "duplicate contact field"} {
		if !strings.Contains(combined, expected) {
			t.Fatalf("warnings do not contain %q: %v", expected, warnings)
		}
	}
}

func TestSchemaRejectsDuplicateAndEmptyKeys(t *testing.T) {
	schema := DefaultSchema()
	schema.SnoozeUntil = schema.LastContact
	if err := schema.Validate(); err == nil || !strings.Contains(err.Error(), "same key") {
		t.Fatalf("duplicate validation error = %v", err)
	}
	schema = DefaultSchema()
	schema.DisplayName = ""
	if err := schema.Validate(); err == nil || !strings.Contains(err.Error(), "cannot be empty") {
		t.Fatalf("empty validation error = %v", err)
	}
}

func date(t *testing.T, year int, month time.Month, day int) time.Time {
	t.Helper()
	return time.Date(year, month, day, 0, 0, 0, 0, time.Local)
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
