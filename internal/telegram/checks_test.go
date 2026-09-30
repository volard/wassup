package telegramsync

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"wassup/internal/contacts"
)

func TestCheckIntervalAndFailedAttemptBackoff(t *testing.T) {
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		status LinkStatus
		days   int
		due    bool
	}{
		{"legacy", LinkStatus{}, 14, true},
		{"recent", LinkStatus{CheckedAt: now.AddDate(0, 0, -13)}, 14, false},
		{"due", LinkStatus{CheckedAt: now.AddDate(0, 0, -14)}, 14, true},
		{"failed attempt", LinkStatus{CheckedAt: now.AddDate(0, 0, -60), LastAttemptAt: now.AddDate(0, 0, -1), LastError: "offline"}, 14, false},
		{"manual only", LinkStatus{}, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.status.Due(now, tc.days); got != tc.due {
				t.Fatal(got)
			}
		})
	}
}
func cachedFixture(t *testing.T) (Service, string, LinkStatus) {
	t.Helper()
	config := filepath.Join(t.TempDir(), "config.json")
	release, err := lockState(StateDir(config))
	if err != nil {
		t.Fatal(err)
	}
	release()
	note := filepath.Join(t.TempDir(), "person.md")
	status := LinkStatus{Person: Person{ID: 42, Username: "alex"}, State: LinkValid, CheckedAt: time.Now().AddDate(0, 0, -20), LastAttemptAt: time.Now().AddDate(0, 0, -20)}
	if err := writeJSON(filepath.Join(StateDir(config), "links.json"), links{AccountID: 1, Notes: map[string]Person{note: status.Person}}); err != nil {
		t.Fatal(err)
	}
	return Service{ConfigPath: config, Schema: contacts.DefaultSchema()}, note, status
}
func TestSnapshotRestoresCacheAndRejectsOtherAccount(t *testing.T) {
	s, note, status := cachedFixture(t)
	if err := s.persistChecks(map[string]LinkStatus{note: status}, 1); err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.Snapshot()
	if err != nil || snapshot[note].State != LinkValid || snapshot[note].CheckedAt.IsZero() {
		t.Fatal(snapshot, err)
	}
	if err := s.persistChecks(map[string]LinkStatus{note: status}, 2); err == nil {
		t.Fatal("wrong account accepted")
	}
	status.Person.ID = 99
	if err := s.persistChecks(map[string]LinkStatus{note: status}, 1); err != nil {
		t.Fatal(err)
	}
	snapshot, _ = s.Snapshot()
	if snapshot[note].Person.ID != 42 {
		t.Fatal("wrong linked identity cached")
	}
}
func TestFailedCheckKeepsOldResultAndRecordsAttempt(t *testing.T) {
	s, note, old := cachedFixture(t)
	if err := s.persistChecks(map[string]LinkStatus{note: old}, 1); err != nil {
		t.Fatal(err)
	}
	results, err := s.Check(context.Background(), []contacts.Contact{{Path: note}})
	if err == nil || !strings.Contains(err.Error(), "login") {
		t.Fatal(err)
	}
	status := results[note]
	if !status.CheckedAt.Equal(old.CheckedAt) || status.LastError == "" || status.LastAttemptAt.Before(time.Now().Add(-time.Minute)) {
		t.Fatal(status)
	}
	snapshot, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot[note].Due(time.Now(), 14) || snapshot[note].LastError == "" {
		t.Fatal("failure not cached")
	}
}
