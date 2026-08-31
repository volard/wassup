package notifications

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"wassup/internal/contacts"
)

func TestBuildDigestChoosesOldestDueContact(t *testing.T) {
	today := localDate(2026, time.August, 30)
	old := localDate(2025, time.January, 1)
	recent := localDate(2026, time.August, 1)
	all := []contacts.Contact{
		{Name: "Recent", Tracked: true, LastContact: &recent, EveryDays: 7},
		{Name: "Oldest", Tracked: true, LastContact: &old, EveryDays: 30},
		{Name: "Information only"},
	}
	digest, exists := BuildDigest(all, today, 1)
	if !exists || digest.DueCount != 2 {
		t.Fatalf("digest=%#v exists=%v", digest, exists)
	}
	if len(digest.Suggestions) != 1 || digest.Suggestions[0].Name != "Oldest" {
		t.Fatalf("suggestions=%#v", digest.Suggestions)
	}
	if digest.Title != "Wassup: 2 contacts due" || digest.Body != "Today's suggestion: Oldest" {
		t.Fatalf("digest=%#v", digest)
	}
}

func TestBuildDigestSkipsUpcomingAndUnscheduledContacts(t *testing.T) {
	today := localDate(2026, time.August, 30)
	yesterday := localDate(2026, time.August, 29)
	all := []contacts.Contact{
		{Name: "Upcoming", Tracked: true, LastContact: &yesterday, EveryDays: 30},
		{Name: "Information only"},
	}
	if digest, exists := BuildDigest(all, today, 1); exists {
		t.Fatalf("unexpected digest: %#v", digest)
	}
}

func TestSendOnceDeduplicatesByLocalDate(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "nested", "state")
	today := localDate(2026, time.August, 30)
	digest := Digest{Title: "Title", Body: "Body"}
	command := []string{"true", "{title}", "{body}"}
	sent, err := SendOnce(command, digest, statePath, today)
	if err != nil || !sent {
		t.Fatalf("first send: sent=%v err=%v", sent, err)
	}
	sent, err = SendOnce(command, digest, statePath, today)
	if err != nil || sent {
		t.Fatalf("duplicate send: sent=%v err=%v", sent, err)
	}
	data, err := os.ReadFile(statePath)
	if err != nil || string(data) != "2026-08-30\n" {
		t.Fatalf("state=%q err=%v", data, err)
	}
}

func TestSystemdUnitsUsePersistentUserTimerAndQuotedPaths(t *testing.T) {
	service, timer := SystemdUnits("/tmp/Wassup App/wassup", "/tmp/100% notes/config.json", "18:00")
	for _, expected := range []string{`ExecStart="/tmp/Wassup App/wassup" --config "/tmp/100%% notes/config.json" --notify`, "After=graphical-session.target"} {
		if !strings.Contains(service, expected) {
			t.Fatalf("service does not contain %q:\n%s", expected, service)
		}
	}
	for _, expected := range []string{"OnCalendar=*-*-* 18:00:00", "Persistent=true", "WantedBy=timers.target"} {
		if !strings.Contains(timer, expected) {
			t.Fatalf("timer does not contain %q:\n%s", expected, timer)
		}
	}
}

func localDate(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.Local)
}
