package telegramsync

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gotd/td/tg"
	"wassup/internal/contacts"
)

func TestCheckedLinkDistinguishesRenamesAndUnavailableAccounts(t *testing.T) {
	saved := Person{ID: 42, AccessHash: 123, Username: "old_name", Phone: "1234567890"}
	current := testUser(42, "Alex")
	current.Username = "new_name"
	current.Phone = "1987654321"
	note := contacts.Contact{Telegram: "@old_name", Phone: "+1234567890"}
	result := checkedLink(note, saved, current, time.Now())
	if result.State != LinkChanged || result.Person.ID != 42 || result.Person.Username != "new_name" {
		t.Fatal(result)
	}
	note.Telegram = "https://t.me/new_name"
	note.Phone = "+1987654321"
	if result := checkedLink(note, saved, current, time.Now()); result.State != LinkValid {
		t.Fatal(result)
	}
	current.Phone = "" // hidden phone does not mean the number is wrong
	if result := checkedLink(note, saved, current, time.Now()); result.State != LinkValid {
		t.Fatal(result)
	}
	current.Deleted = true
	if result := checkedLink(note, saved, current, time.Now()); result.State != LinkInvalid {
		t.Fatal(result)
	}
	if result := checkedLink(note, saved, nil, time.Now()); result.State != LinkInvalid {
		t.Fatal(result)
	}
	if result := checkedLink(note, saved, testUser(999, "Other"), time.Now()); result.State != LinkInvalid {
		t.Fatal(result)
	}
}
func TestSaveResolvedUpdatesNoteAndStableLink(t *testing.T) {
	dir := t.TempDir()
	path := fixture(t, dir, "Alex", "2025-01-01")
	saved := links{AccountID: 1, Notes: map[string]Person{}}
	person := Person{ID: 42, AccessHash: 123, Username: "new_name", Phone: "1987654321"}
	candidate := Candidate{Person: person, AccountID: 1, Reference: "@new_name"}
	linksPath := filepath.Join(dir, "links.json")
	if err := saveResolved(contacts.Contact{Path: path}, candidate, person, &saved, linksPath, contacts.DefaultSchema()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	for _, want := range []string{`cnt:telegram: "@new_name"`, `cnt:phone: "+1987654321"`, "cnt:last-contact: 2025-01-01\r\n", "cnt:snooze-until: 2025-12-01\r\n", "Private note stays verbatim.\r\n"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("missing %q", want)
		}
	}
	loaded, err := loadLinks(linksPath, 1)
	if err != nil || loaded.Notes[path].ID != 42 {
		t.Fatal(loaded, err)
	}
	// A later username removal clears the stale field, while a hidden phone stays.
	person.Username = ""
	person.Phone = ""
	candidate.Person = person
	if err := saveResolved(contacts.Contact{Path: path}, candidate, person, &saved, linksPath, contacts.DefaultSchema()); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), "cnt:telegram:") || !strings.Contains(string(data), `cnt:phone: "+1987654321"`) {
		t.Fatal(string(data))
	}
}
func TestSaveRejectsChangedProfileAndDuplicateLinks(t *testing.T) {
	dir := t.TempDir()
	path := fixture(t, dir, "Alex", "")
	before, _ := os.ReadFile(path)
	original := Person{ID: 42, Username: "original"}
	changed := original
	changed.Username = "changed"
	saved := links{AccountID: 1, Notes: map[string]Person{}}
	if err := saveResolved(contacts.Contact{Path: path}, Candidate{Person: original}, changed, &saved, filepath.Join(dir, "links.json"), contacts.DefaultSchema()); err == nil {
		t.Fatal("saved changed identity")
	}
	saved.Notes = map[string]Person{filepath.Join(dir, "other.md"): original}
	if err := saveResolved(contacts.Contact{Path: path}, Candidate{Person: original}, original, &saved, filepath.Join(dir, "links.json"), contacts.DefaultSchema()); err == nil {
		t.Fatal("duplicate account accepted")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Fatal("failed save modified note")
	}
}
func TestServiceWithoutLoginIsNoninteractive(t *testing.T) {
	s := Service{ConfigPath: filepath.Join(t.TempDir(), "config.json"), Schema: contacts.DefaultSchema()}
	snapshot, err := s.Snapshot()
	if err != nil || len(snapshot) != 0 {
		t.Fatal(snapshot, err)
	}
	_, err = s.Resolve(context.Background(), contacts.Contact{}, "@alex", false)
	if err == nil || !strings.Contains(err.Error(), "login") {
		t.Fatal(err)
	}
	if _, err := os.Stat(StateDir(s.ConfigPath)); !os.IsNotExist(err) {
		t.Fatal("created state without login")
	}
}
func TestBatchValidationRequestsStoredUserIDs(t *testing.T) {
	api := &fakeAPI{users: []tg.UserClass{testUser(42, "Renamed"), &tg.UserEmpty{ID: 43}}}
	remote := &remoteClient{api: api}
	users, err := remote.checkPeople(context.Background(), []Person{{ID: 42, AccessHash: 123}, {ID: 43, AccessHash: 456}})
	if err != nil || len(users) != 1 {
		t.Fatal(users, err)
	}
	if len(api.userRequest) != 2 || api.userRequest[0].(*tg.InputUser).AccessHash != 123 || api.userRequest[1].(*tg.InputUser).UserID != 43 {
		t.Fatal(api.userRequest)
	}
}

type phoneAPI struct {
	telegramAPI
	number string
}

func (f *phoneAPI) ContactsResolvePhone(_ context.Context, number string) (*tg.ContactsResolvedPeer, error) {
	f.number = number
	user := testUser(42, "Alex")
	user.Phone = number
	return &tg.ContactsResolvedPeer{Peer: &tg.PeerUser{UserID: 42}, Users: []tg.UserClass{user}}, nil
}
func TestPhoneLookupUsesNormalizedNumber(t *testing.T) {
	api := &phoneAPI{}
	remote := &remoteClient{api: api}
	person, err := remote.resolveReference(context.Background(), "+1 (234) 567-8901")
	if err != nil || person.ID != 42 || api.number != "12345678901" {
		t.Fatal(person, api.number, err)
	}
}
