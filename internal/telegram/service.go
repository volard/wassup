package telegramsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"wassup/internal/contacts"
)

type LinkState int

const (
	LinkUnknown LinkState = iota
	LinkValid
	LinkChanged
	LinkInvalid
)

type LinkStatus struct {
	Person        Person
	State         LinkState
	Detail        string
	CheckedAt     time.Time
	LastAttemptAt time.Time
	LastError     string
}

type Candidate struct {
	Person    Person
	AccountID int64
	Reference string
	IsPhone   bool
}

// Service exposes non-interactive account operations for the TUI. Only the
// separate login command is allowed to request authentication credentials.
type Service struct {
	ConfigPath string
	Schema     contacts.Schema
}

func (s Service) Snapshot() (map[string]LinkStatus, error) {
	var saved links
	err := readJSON(filepath.Join(StateDir(s.ConfigPath), "links.json"), &saved)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]LinkStatus{}, nil
	}
	if err != nil {
		return nil, err
	}
	if saved.AccountID <= 0 {
		return nil, errors.New("invalid Telegram account in saved links")
	}
	result := map[string]LinkStatus{}
	cached, err := readChecks(s.ConfigPath, saved.AccountID)
	if err != nil {
		return nil, err
	}
	for path, p := range saved.Notes {
		result[path] = LinkStatus{Person: p, State: LinkUnknown, Detail: "Saved link; not checked this session"}
		if status, ok := cached.Results[path]; ok && status.Person.ID == p.ID {
			result[path] = status
		}
	}
	return result, nil
}

func (s Service) withAccount(ctx context.Context, fn func(context.Context, *remoteClient, *links, string) error) error {
	ReportProgress(ctx, Progress{Phase: "Opening saved session"})
	dir := StateDir(s.ConfigPath)
	// Do not create state or start authentication from a background TUI action.
	var creds credentials
	if err := readJSON(filepath.Join(dir, "credentials.json"), &creds); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("Telegram is not connected; run wassup telegram login first")
		}
		return err
	}
	if err := creds.validate(); err != nil {
		return err
	}
	if _, err := privateRead(filepath.Join(dir, "session.json")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("Telegram session is missing; run wassup telegram login")
		}
		return err
	}
	unlock, err := lockState(dir)
	if err != nil {
		return err
	}
	defer unlock()
	client := telegram.NewClient(creds.AppID, creds.AppHash, telegram.Options{
		SessionStorage: sessionStorage{filepath.Join(dir, "session.json")}, NoUpdates: true,
		Device: telegram.DeviceConfig{DeviceModel: "Wassup", SystemVersion: "Desktop", AppVersion: "1.0", SystemLangCode: "en", LangCode: "en"},
	})
	ReportProgress(ctx, Progress{Phase: "Connecting to Telegram (20s limit)"})
	connectionCtx, cancelConnection := context.WithCancelCause(ctx)
	defer cancelConnection(nil)
	connectionTimer := time.AfterFunc(20*time.Second, func() { cancelConnection(errors.New("Telegram connection timed out after 20 seconds")) })
	defer connectionTimer.Stop()
	err = client.Run(connectionCtx, func(ctx context.Context) error {
		connectionTimer.Stop()
		ReportProgress(ctx, Progress{Phase: "Checking authorization (20s limit)"})
		authCtx, cancelAuth := context.WithTimeout(ctx, 20*time.Second)
		status, err := client.Auth().Status(authCtx)
		cancelAuth()
		if err != nil {
			return err
		}
		if !status.Authorized || status.User == nil || status.User.Bot {
			return errors.New("Telegram session expired; run wassup telegram login")
		}
		path := filepath.Join(dir, "links.json")
		saved, err := loadLinks(path, status.User.ID)
		if err != nil {
			return err
		}
		return fn(ctx, &remoteClient{api: client.API()}, &saved, path)
	})
	if cause := context.Cause(connectionCtx); cause != nil {
		return cause
	}
	if err != nil {
		return err
	}
	return ctx.Err()
}

// Check validates in batches by immutable user ID. Username and phone changes
// never redirect an existing link to a newly resolved account.
func (s Service) Check(ctx context.Context, notes []contacts.Contact) (map[string]LinkStatus, error) {
	result, err := s.Snapshot()
	if err != nil {
		return nil, err
	}
	selected := map[string]LinkStatus{}
	for _, note := range notes {
		if status, ok := result[note.Path]; ok {
			selected[note.Path] = status
		}
	}
	result = selected
	if len(result) == 0 {
		return result, nil
	}
	var identity links
	if err := readJSON(filepath.Join(StateDir(s.ConfigPath), "links.json"), &identity); err != nil {
		return nil, err
	}
	attempt := time.Now()
	completed := map[string]bool{}
	for path, status := range result {
		status.LastAttemptAt = attempt
		result[path] = status
	}
	err = s.withAccount(ctx, func(ctx context.Context, remote *remoteClient, saved *links, _ string) error {
		return s.checkProfiles(ctx, remote, saved, notes, result, completed, attempt)
	})
	for path, status := range result {
		status.LastAttemptAt = attempt
		if err != nil && !completed[path] {
			status.LastError = err.Error()
		}
		result[path] = status
	}
	ReportProgress(ctx, Progress{Phase: "Saving check results"})
	cacheErr := s.persistChecks(result, identity.AccountID)
	return result, errors.Join(err, cacheErr)
}

// checkProfiles fetches all remote profiles before importing any note fields.
func (s Service) checkProfiles(ctx context.Context, remote *remoteClient, saved *links, notes []contacts.Contact, result map[string]LinkStatus, completed map[string]bool, attempt time.Time) error {
	profiles := map[string]Profile{}
	verified := map[int64]*tg.User{}
	var linked []contacts.Contact
	for _, note := range notes {
		if p, ok := saved.Notes[note.Path]; ok {
			linked = append(linked, note)
			if old := result[note.Path]; old.Person.ID != p.ID {
				result[note.Path] = LinkStatus{Person: p, LastAttemptAt: attempt}
			}
		}
	}
	for start := 0; start < len(linked); start += 100 {
		ReportProgress(ctx, Progress{Phase: "Checking accounts (20s per batch)", Done: start, Total: len(linked)})
		batch := linked[start:min(start+100, len(linked))]
		people := make([]Person, 0, len(batch))
		for _, note := range batch {
			people = append(people, saved.Notes[note.Path])
		}
		users, err := remote.checkPeople(ctx, people)
		if err != nil {
			return err
		}
		for index, note := range batch {
			p := saved.Notes[note.Path]
			verified[p.ID] = users[p.ID]
			result[note.Path] = checkedLink(note, p, users[p.ID], time.Now())
			status := result[note.Path]
			if status.State == LinkValid || status.State == LinkChanged {
				ReportProgress(ctx, Progress{Phase: "Fetching birthdays and phones", Done: start + index, Total: len(linked)})
				profile, err := remote.Profile(ctx, status.Person)
				if err != nil {
					return err
				}
				profiles[note.Path] = profile
			} else {
				completed[note.Path] = true
			}
		}
		ReportProgress(ctx, Progress{Phase: "Checking accounts", Done: start + len(batch), Total: len(linked)})
	}
	for index, note := range linked {
		profile, ok := profiles[note.Path]
		if !ok {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		ReportProgress(ctx, Progress{Phase: "Saving missing profile details", Done: index, Total: len(linked)})
		fields, err := contacts.ImportTelegram(note.Path, profile.Birthday, profile.Phone, time.Time{}, s.Schema, false)
		if err != nil {
			return err
		}
		completed[note.Path] = true
		if note.Phone == "" {
			note.Phone = profile.Phone
		}
		p := saved.Notes[note.Path]
		result[note.Path] = checkedLink(note, p, verified[p.ID], time.Now())
		if len(fields) > 0 {
			status := result[note.Path]
			status.Detail += "; imported " + strings.Join(fields, ", ")
			result[note.Path] = status
		}
	}
	return nil
}

func (r *remoteClient) checkPeople(ctx context.Context, people []Person) (map[int64]*tg.User, error) {
	inputs := make([]tg.InputUserClass, 0, len(people))
	for _, p := range people {
		inputs = append(inputs, &tg.InputUser{UserID: p.ID, AccessHash: p.AccessHash})
	}
	callCtx, cancel, err := r.requestContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	users, err := r.api.UsersGetUsers(callCtx, inputs)
	if err != nil {
		return nil, err
	}
	result := map[int64]*tg.User{}
	for _, user := range users {
		if u, ok := user.(*tg.User); ok {
			result[u.ID] = u
		}
	}
	return result, nil
}

func checkedLink(note contacts.Contact, saved Person, u *tg.User, now time.Time) LinkStatus {
	status := LinkStatus{Person: saved, State: LinkInvalid, Detail: "Telegram account is deleted or unavailable", CheckedAt: now, LastAttemptAt: now}
	if u == nil || u.ID != saved.ID {
		return status
	}
	if u.Min {
		status.State = LinkUnknown
		status.Detail = "Telegram returned incomplete account details"
		return status
	}
	current, ok := personFromUser(u)
	if !ok {
		return status
	}
	status.Person = current
	status.State = LinkValid
	status.Detail = "Telegram account verified"
	staleHandle := username(note.Telegram) != "" && username(note.Telegram) != username(current.Username)
	stalePhone := phone(note.Phone) != "" && phone(current.Phone) != "" && phone(note.Phone) != phone(current.Phone)
	if staleHandle || stalePhone {
		status.State = LinkChanged
		status.Detail = "Account verified; note username or phone is outdated (t to update)"
	}
	return status
}

func PreferredReference(note contacts.Contact, status LinkStatus) string {
	if status.Person.ID != 0 {
		if status.Person.Username != "" {
			return "@" + status.Person.Username
		}
		if status.Person.Phone != "" {
			return "+" + phone(status.Person.Phone)
		}
		return fmt.Sprintf("id:%d", status.Person.ID)
	}
	if note.Telegram != "" {
		return note.Telegram
	}
	if note.Phone != "" {
		return note.Phone
	}
	return ""
}

func (s Service) Resolve(ctx context.Context, note contacts.Contact, value string, keepLinkedID bool) (Candidate, error) {
	var candidate Candidate
	err := s.withAccount(ctx, func(ctx context.Context, remote *remoteClient, saved *links, _ string) error {
		var p Person
		var err error
		if old, exists := saved.Notes[note.Path]; keepLinkedID && exists {
			ReportProgress(ctx, Progress{Phase: "Looking up saved account ID"})
			users, err := remote.checkPeople(ctx, []Person{old})
			if err != nil {
				return err
			}
			status := checkedLink(note, old, users[old.ID], time.Now())
			if status.State != LinkValid && status.State != LinkChanged {
				return errors.New(status.Detail + "; enter a new username or phone to relink")
			}
			p = status.Person
		} else {
			p, err = remote.resolveReference(ctx, value)
			if err != nil {
				return err
			}
		}
		candidate = Candidate{Person: p, AccountID: saved.AccountID}
		// Persist the live username when available; otherwise use a visible phone.
		if p.Username != "" {
			candidate.Reference = "@" + p.Username
		} else if p.Phone != "" {
			candidate.Reference = "+" + phone(p.Phone)
			candidate.IsPhone = true
		}
		return nil
	})
	return candidate, err
}

func (r *remoteClient) resolveReference(ctx context.Context, value string) (Person, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return Person{}, errors.New("enter a Telegram username or phone number")
	}
	// Telegram usernames contain a letter or underscore. Numeric input is a phone,
	// never an account ID, to avoid ambiguity with phone fields in existing notes.
	if number := phone(value); number != "" {
		ReportProgress(ctx, Progress{Phase: "Looking up phone (3s pacing + 20s limit)"})
		// Each operation holds the profile lock. Waiting here also spaces lookups
		// made by separate short-lived TUI connections.
		r.nextCall = time.Now().Add(3 * time.Second)
		callCtx, cancel, err := r.requestContext(ctx)
		if err != nil {
			return Person{}, err
		}
		defer cancel()
		result, err := r.api.ContactsResolvePhone(callCtx, number)
		// Phone lookups are limited to at most one every three seconds.
		r.nextCall = time.Now().Add(3 * time.Second)
		if err != nil {
			return Person{}, fmt.Errorf("could not resolve phone (Telegram privacy settings may prevent lookup): %w", err)
		}
		peer, ok := result.Peer.(*tg.PeerUser)
		if !ok {
			return Person{}, errors.New("phone does not identify a personal account")
		}
		for _, user := range result.Users {
			if u, ok := user.(*tg.User); ok && u.ID == peer.UserID {
				if p, ok := personFromUser(u); ok {
					return p, nil
				}
			}
		}
		return Person{}, errors.New("phone identifies an unavailable account")
	}
	ReportProgress(ctx, Progress{Phase: "Looking up username (20s limit)"})
	return r.Resolve(ctx, value)
}

func (s Service) Save(ctx context.Context, note contacts.Contact, candidate Candidate) (LinkStatus, error) {
	var status LinkStatus
	err := s.withAccount(ctx, func(ctx context.Context, remote *remoteClient, saved *links, path string) error {
		if saved.AccountID != candidate.AccountID {
			return errors.New("Telegram account changed; resolve the contact again")
		}
		ReportProgress(ctx, Progress{Phase: "Verifying selected account"})
		users, err := remote.checkPeople(ctx, []Person{candidate.Person})
		if err != nil {
			return err
		}
		status = checkedLink(note, candidate.Person, users[candidate.Person.ID], time.Now())
		if status.State != LinkValid && status.State != LinkChanged {
			return errors.New(status.Detail)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		ReportProgress(ctx, Progress{Phase: "Saving link and note details"})
		if err := saveResolved(note, candidate, status.Person, saved, path, s.Schema); err != nil {
			return err
		}
		// The note may contain a second, outdated identity field; show its real state.
		updated, _ := contacts.Scan(filepath.Dir(note.Path), s.Schema)
		for _, current := range updated {
			if current.Path == note.Path {
				status = checkedLink(current, status.Person, users[status.Person.ID], time.Now())
				break
			}
		}
		cache, err := readChecks(s.ConfigPath, saved.AccountID)
		if err != nil {
			return err
		}
		cache.Results[note.Path] = status
		if err := writeJSON(checksPath(s.ConfigPath), cache); err != nil {
			return fmt.Errorf("link saved but validation cache failed: %w", err)
		}
		return nil
	})
	return status, err
}

func saveResolved(note contacts.Contact, candidate Candidate, current Person, saved *links, path string, schema contacts.Schema) error {
	if err := saved.bind(note.Path, current); err != nil {
		return err
	}
	if current.Username != candidate.Person.Username || current.Phone != candidate.Person.Phone {
		return errors.New("Telegram details changed; resolve and confirm again")
	}
	number := ""
	if phone(current.Phone) != "" {
		number = "+" + phone(current.Phone)
	}
	if err := contacts.SetTelegramIdentity(note.Path, current.Username, number, schema); err != nil {
		return err
	}

	if err := writeJSON(path, saved); err != nil {
		return fmt.Errorf("note details saved, but Telegram link could not be saved: %w", err)
	}
	return nil
}
