package telegramsync

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gotd/td/tg"
	"wassup/internal/contacts"
)

type profileAPI struct {
	telegramAPI
	full      *tg.UsersUserFull
	err       error
	requested *tg.InputUser
}

func (f *profileAPI) UsersGetFullUser(_ context.Context, id tg.InputUserClass) (*tg.UsersUserFull, error) {
	f.requested = id.(*tg.InputUser)
	return f.full, f.err
}
func fullProfile() *tg.UsersUserFull {
	u := &tg.User{ID: 42, Phone: "12345678900", Username: "somebody"}
	u.SetAccessHash(123)
	return &tg.UsersUserFull{FullUser: tg.UserFull{ID: 42}, Users: []tg.UserClass{u}}
}
func TestProfilePrivacyAndBirthday(t *testing.T) {
	for _, tc := range []struct {
		name      string
		birthday  *tg.Birthday
		hidePhone bool
		want      string
		invalid   bool
	}{
		{name: "hidden", hidePhone: true},
		{name: "month day", birthday: &tg.Birthday{Day: 29, Month: 2}, want: "--02-29"},
		{name: "full date", birthday: func() *tg.Birthday { b := &tg.Birthday{Day: 29, Month: 2}; b.SetYear(2000); return b }(), want: "2000-02-29"},
		{name: "invalid", birthday: &tg.Birthday{Day: 31, Month: 2}, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			full := fullProfile()
			if tc.birthday != nil {
				full.FullUser.SetBirthday(*tc.birthday)
			}
			if tc.hidePhone {
				full.Users[0].(*tg.User).Phone = ""
			}
			api := &profileAPI{full: full}
			p, err := (&remoteClient{api: api}).Profile(context.Background(), Person{ID: 42, AccessHash: 123})
			if (err != nil) != tc.invalid {
				t.Fatal(err)
			}
			if err != nil {
				return
			}
			if p.Birthday != tc.want {
				t.Fatal(p)
			}
			if tc.hidePhone && p.Phone != "" {
				t.Fatal("hidden phone imported")
			}
			if !tc.hidePhone && p.Phone != "+12345678900" {
				t.Fatal(p)
			}
			if api.requested.UserID != 42 || api.requested.AccessHash != 123 {
				t.Fatal(api.requested)
			}
		})
	}
	for _, mode := range []string{"different ID", "missing user", "deleted", "rpc"} {
		t.Run(mode, func(t *testing.T) {
			api := &profileAPI{full: fullProfile()}
			switch mode {
			case "different ID":
				api.full.FullUser.ID = 99
			case "missing user":
				api.full.Users = nil
			case "deleted":
				api.full.Users[0].(*tg.User).Deleted = true
			case "rpc":
				api.err = errors.New("offline")
			}
			if _, err := (&remoteClient{api: api}).Profile(context.Background(), Person{ID: 42}); err == nil {
				t.Fatal("invalid profile accepted")
			}
		})
	}
}
func TestSyncImportsProfileWithoutMessagesAndPreservesExisting(t *testing.T) {
	dir := t.TempDir()
	path := fixture(t, dir, "Person", "")
	schema := contacts.DefaultSchema()
	found, _ := contacts.Scan(dir, schema)
	original, _ := os.ReadFile(path)
	remote := &fakeHistory{profiles: map[int64]Profile{42: {Birthday: "--02-29", Phone: "+99999999999"}}}
	saved := links{Notes: map[string]Person{path: {ID: 42}}}
	var out bytes.Buffer
	if err := syncNotes(context.Background(), remote, found, saved, schema, true, &out); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(original, after) || !strings.Contains(out.String(), "birthday: --02-29") {
		t.Fatal(out.String())
	}
	if err := syncNotes(context.Background(), remote, found, saved, schema, false, &out); err != nil {
		t.Fatal(err)
	}
	current, _ := contacts.Scan(dir, schema)
	if current[0].BirthDate != "--02-29" || current[0].Phone != found[0].Phone || current[0].LastContact != nil {
		t.Fatal(current[0])
	}
	remote.profiles[42] = Profile{Birthday: "1990-01-01"}
	out.Reset()
	if err := syncNotes(context.Background(), remote, current, saved, schema, false, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Updated 0 notes") {
		t.Fatal(out.String())
	}
}
func TestCheckProfilesImportsAndReportsProgress(t *testing.T) {
	dir := t.TempDir()
	path := fixture(t, dir, "Person", "")
	schema := contacts.DefaultSchema()
	full := fullProfile()
	full.FullUser.SetBirthday(tg.Birthday{Day: 12, Month: 5})
	api := &profileAPI{telegramAPI: &fakeAPI{users: full.Users}, full: full}
	notes, _ := contacts.Scan(dir, schema)
	results := map[string]LinkStatus{}
	completed := map[string]bool{}
	var phases []string
	ctx := WithProgress(context.Background(), func(p Progress) { phases = append(phases, p.Phase) })
	err := (Service{Schema: schema}).checkProfiles(ctx, &remoteClient{api: api}, &links{Notes: map[string]Person{path: {ID: 42, AccessHash: 123}}}, notes, results, completed, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	current, _ := contacts.Scan(dir, schema)
	if current[0].BirthDate != "--05-12" || !completed[path] || !strings.Contains(results[path].Detail, "imported birthday") {
		t.Fatal(current, results, completed)
	}
	if !strings.Contains(strings.Join(phases, ";"), "Fetching birthdays and phones") {
		t.Fatal(phases)
	}
}

func TestProfileFailureLeavesWholeSyncUnwritten(t *testing.T) {
	dir := t.TempDir()
	schema := contacts.DefaultSchema()
	first := fixture(t, dir, "A", "")
	second := fixture(t, dir, "B", "")
	original, _ := os.ReadFile(first)
	found, _ := contacts.Scan(dir, schema)
	remote := &fakeHistory{profiles: map[int64]Profile{1: {Birthday: "--05-12"}}, profileFailID: 2, dates: map[int64]time.Time{1: time.Now()}}
	err := syncNotes(context.Background(), remote, found, links{Notes: map[string]Person{first: {ID: 1}, second: {ID: 2}}}, schema, false, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected profile failure")
	}
	after, _ := os.ReadFile(first)
	if !bytes.Equal(original, after) {
		t.Fatal("wrote note before fetching all profiles")
	}
}
