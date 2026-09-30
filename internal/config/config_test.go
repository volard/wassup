package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadOrCreateWritesDefaultsThenLoadsThem(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "config.json")
	defaults := Default("/notes/contacts")

	createdConfig, created, err := LoadOrCreate(path, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if !created || !reflect.DeepEqual(createdConfig, defaults) {
		t.Fatalf("created=%v config=%#v", created, createdConfig)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions = %o, want 600", info.Mode().Perm())
	}

	loadedConfig, createdAgain, err := LoadOrCreate(path, Default("/different"))
	if err != nil {
		t.Fatal(err)
	}
	if createdAgain || !reflect.DeepEqual(loadedConfig, defaults) {
		t.Fatalf("existing config was replaced: created=%v config=%#v", createdAgain, loadedConfig)
	}
}

func TestRelativeContactsDirResolvesFromConfigDirectory(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "settings", "config.json")
	resolved, err := Default("../../contacts").ResolveContactsDir(configPath)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Clean(filepath.Join(filepath.Dir(configPath), "../../contacts"))
	if resolved != want {
		t.Fatalf("resolved = %q, want %q", resolved, want)
	}
}

func TestLoadRejectsUnknownAndInvalidFields(t *testing.T) {
	for name, content := range map[string]string{
		"unknown": `{"contacts_dir":"/tmp","frontmatter_fields":{},"typo":true}`,
		"view": `{
  "contacts_dir": "/tmp",
  "start_view": "friends",
  "frontmatter_fields": {
    "display_name": "name",
    "fallback_name": "alias",
    "last_contact": "last",
    "contact_every_days": "every",
    "snooze_until": "snooze"
  }
}`,
		"opener": `{
  "contacts_dir": "/tmp",
  "start_view": "all",
  "opener": {"command": ["nvim"]},
  "frontmatter_fields": {
    "display_name": "name",
    "fallback_name": "alias",
    "last_contact": "last",
    "contact_every_days": "every",
    "snooze_until": "snooze"
  }
}`,
		"invalid": `{
  "contacts_dir": "/tmp",
  "frontmatter_fields": {
    "display_name": "name",
    "fallback_name": "name",
    "last_contact": "last",
    "contact_every_days": "every",
    "snooze_until": "snooze"
  }
}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			_, _, err := LoadOrCreate(path, Default("/fallback"))
			if err == nil {
				t.Fatal("expected invalid config error")
			}
			if name == "unknown" && !strings.Contains(err.Error(), "unknown field") {
				t.Fatalf("error = %v", err)
			}
			if name == "invalid" && !strings.Contains(err.Error(), "same key") {
				t.Fatalf("error = %v", err)
			}
			if name == "view" && !strings.Contains(err.Error(), "start_view") {
				t.Fatalf("error = %v", err)
			}
			if name == "opener" && !strings.Contains(err.Error(), "{path}") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestLegacyConfigGetsDefaultStartView(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	content := `{
  "contacts_dir": "/tmp",
  "frontmatter_fields": {
    "display_name": "name",
    "fallback_name": "alias",
    "last_contact": "last",
    "contact_every_days": "every",
    "snooze_until": "snooze"
  }
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got, created, err := LoadOrCreate(path, Default("/fallback"))
	if err != nil {
		t.Fatal(err)
	}
	if created || got.StartView != DefaultStartView || !reflect.DeepEqual(got.Opener.Command, []string{"nvim", "{path}"}) {
		t.Fatalf("created=%v start_view=%q opener=%v", created, got.StartView, got.Opener.Command)
	}
	if got.Fields.BirthDate != "cnt:birth-date" || got.Fields.RelationshipOrigin != "rel:origin" || len(got.Fields.RelationshipOrigins) == 0 {
		t.Fatalf("legacy schema was not upgraded: %#v", got.Fields)
	}
	if got.Notifications.DailyAt != "18:00" || got.Notifications.MaxSuggestions != 1 || len(got.Notifications.Command) == 0 {
		t.Fatalf("legacy notification settings were not upgraded: %#v", got.Notifications)
	}
}

func TestNotificationSettingsValidation(t *testing.T) {
	for name, change := range map[string]func(*Config){
		"time":         func(value *Config) { value.Notifications.DailyAt = "evening" },
		"suggestions":  func(value *Config) { value.Notifications.MaxSuggestions = 0 },
		"missing body": func(value *Config) { value.Notifications.Command = []string{"notify-send", "{title}"} },
	} {
		t.Run(name, func(t *testing.T) {
			value := Default("/contacts")
			change(&value)
			if err := value.Validate(); err == nil || !strings.Contains(err.Error(), "notifications") {
				t.Fatalf("validation error = %v", err)
			}
		})
	}
}

func TestTelegramCheckIntervalDefaultsAndExplicitValues(t *testing.T) {
	for _, tc := range []struct {
		field   string
		want    int
		invalid bool
	}{
		{"", 14, false},
		{`,"telegram":{"check_every_days":7}`, 7, false},
		{`,"telegram":{"check_every_days":0}`, 0, false},
		{`,"telegram":{"check_every_days":-1}`, 0, true},
	} {
		path := filepath.Join(t.TempDir(), "config.json")
		data := `{"contacts_dir":"/tmp","frontmatter_fields":{"display_name":"name","fallback_name":"alias","last_contact":"last","contact_every_days":"every","snooze_until":"snooze"}` + tc.field + `}`
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		result, _, err := LoadOrCreate(path, Default("/tmp"))
		if tc.invalid {
			if err == nil {
				t.Fatal("invalid interval accepted")
			}
			continue
		}
		if err != nil || result.Telegram.CheckEveryDays != tc.want {
			t.Fatal(result.Telegram, err)
		}
	}
}
