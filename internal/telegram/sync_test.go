package telegramsync

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"wassup/internal/contacts"
)

type fakeHistory struct {
	profiles      map[int64]Profile
	profileFailID int64
	dates         map[int64]time.Time
	failID        int64
	calls         []int64
	before        func()
}

func (f *fakeHistory) Profile(_ context.Context, p Person) (Profile, error) {
	if p.ID == f.profileFailID {
		return Profile{}, errors.New("profile unavailable")
	}
	return f.profiles[p.ID], nil
}

func (f *fakeHistory) LatestOutgoing(_ context.Context, p Person) (time.Time, error) {
	f.calls = append(f.calls, p.ID)
	if f.before != nil {
		f.before()
		f.before = nil
	}
	if p.ID == f.failID {
		return time.Time{}, errors.New("FLOOD_WAIT_30")
	}
	return f.dates[p.ID], nil
}
func fixture(t *testing.T, dir, name, last string) string {
	t.Helper()
	path := filepath.Join(dir, name+".md")
	content := "---\r\ncnt:name: " + name + "\r\ncnt:telegram: '@somebody'\r\ncnt:phone: '+1 (234) 567-8900'\r\ncnt:snooze-until: 2025-12-01\r\ncustom: keep\r\n"
	if last != "" {
		content += "cnt:last-contact: " + last + "\r\n"
	}
	content += "---\r\n# " + name + "\r\nPrivate note stays verbatim.\r\n"
	if err := os.WriteFile(path, []byte(content), 0640); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestSyncPreviewApplyAndRepeat(t *testing.T) {
	dir := t.TempDir()
	schema := contacts.DefaultSchema()
	changed := fixture(t, dir, "Changed", "2025-01-01")
	newer := fixture(t, dir, "Newer", "2025-06-01")
	noOutgoing := fixture(t, dir, "NoOutgoing", "")
	unlinked := fixture(t, dir, "Unlinked", "")
	original, _ := os.ReadFile(changed)
	untouched := map[string][]byte{}
	for _, path := range []string{newer, noOutgoing, unlinked} {
		untouched[path], _ = os.ReadFile(path)
	}
	found, warnings := contacts.Scan(dir, schema)
	if len(warnings) != 0 {
		t.Fatal(warnings)
	}
	if found[0].Telegram != "@somebody" || found[0].Phone != "+1 (234) 567-8900" {
		t.Fatal("missing parsed identity fields")
	}
	l := links{Notes: map[string]Person{changed: {ID: 1}, newer: {ID: 2}, noOutgoing: {ID: 3}}}
	date := time.Date(2025, 2, 3, 12, 0, 0, 0, time.Local)
	remote := &fakeHistory{dates: map[int64]time.Time{1: date, 2: date}}
	var out bytes.Buffer
	if err := syncNotes(context.Background(), remote, found, l, schema, true, &out); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(changed)
	if !bytes.Equal(data, original) || !strings.Contains(out.String(), "Would update 1 notes") {
		t.Fatal(out.String())
	}
	out.Reset()
	if err := syncNotes(context.Background(), remote, found, l, schema, false, &out); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(changed)
	expected := bytes.Replace(original, []byte("cnt:last-contact: 2025-01-01"), []byte("cnt:last-contact: 2025-02-03"), 1)
	if !bytes.Equal(data, expected) {
		t.Fatalf("unexpected rewrite: %s", data)
	}
	info, _ := os.Stat(changed)
	if info.Mode().Perm() != 0640 {
		t.Fatal("permissions changed")
	}
	for path, before := range untouched {
		after, _ := os.ReadFile(path)
		if !bytes.Equal(before, after) {
			t.Fatalf("changed %s", path)
		}
	}
	found, _ = contacts.Scan(dir, schema)
	out.Reset()
	if err := syncNotes(context.Background(), remote, found, l, schema, false, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Updated 0 notes") {
		t.Fatal(out.String())
	}
}
func TestSyncRemoteFailureWritesNothing(t *testing.T) {
	dir := t.TempDir()
	schema := contacts.DefaultSchema()
	a := fixture(t, dir, "A", "")
	b := fixture(t, dir, "B", "")
	before, _ := os.ReadFile(a)
	found, _ := contacts.Scan(dir, schema)
	remote := &fakeHistory{dates: map[int64]time.Time{1: time.Date(2025, 1, 2, 12, 0, 0, 0, time.Local)}, failID: 2}
	err := syncNotes(context.Background(), remote, found, links{Notes: map[string]Person{a: {ID: 1}, b: {ID: 2}}}, schema, false, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "no note updates applied") {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(a)
	if !bytes.Equal(before, after) {
		t.Fatal("partial update on API error")
	}
}
func TestSyncRereadsNewerLocalDate(t *testing.T) {
	dir := t.TempDir()
	schema := contacts.DefaultSchema()
	path := fixture(t, dir, "A", "2025-01-01")
	found, _ := contacts.Scan(dir, schema)
	remote := &fakeHistory{dates: map[int64]time.Time{1: time.Date(2025, 2, 1, 12, 0, 0, 0, time.Local)}, before: func() {
		_, err := contacts.AdvanceLastContact(path, time.Date(2025, 3, 1, 0, 0, 0, 0, time.Local), schema)
		if err != nil {
			t.Fatal(err)
		}
	}}
	if err := syncNotes(context.Background(), remote, found, links{Notes: map[string]Person{path: {ID: 1}}}, schema, false, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !bytes.Contains(data, []byte("2025-03-01")) {
		t.Fatal("overwrote newer local date")
	}
}
func TestUsernameAndPhoneMatching(t *testing.T) {
	for _, v := range []string{"@Somebody", "Somebody", "https://t.me/Somebody", "t.me/Somebody", "https://telegram.me/Somebody/"} {
		if username(v) != "somebody" {
			t.Fatalf("username %q", v)
		}
	}
	for _, v := range []string{"https://evil.example/Somebody", "https://t.me/+invite", "https://t.me/name/123", "[username]"} {
		if username(v) != "" {
			t.Fatalf("accepted %q", v)
		}
	}
	people := []Person{{ID: 1, Username: "Somebody"}, {ID: 2, Phone: "12345678900"}, {ID: 3, Name: "Alex"}, {ID: 4, Name: "Someone else"}}
	got := suggestions(contacts.Contact{Name: "Alex", Telegram: "https://t.me/Somebody", Phone: "+1 (234) 567-8900"}, people)
	if len(got) != 3 || got[0].ID != 1 || got[1].ID != 2 || got[2].ID != 3 {
		t.Fatal(got)
	}
	if phone("unknown") != "" || phone("+123 ext 45") != "" {
		t.Fatal("invalid phone accepted")
	}
}
