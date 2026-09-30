package contacts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestImportTelegramPreservesFieldsAndSupportsCustomSchema(t *testing.T) {
	schema := DefaultSchema()
	schema.Phone = "mobile"
	schema.BirthDate = "birthday"
	path := filepath.Join(t.TempDir(), "person.md")
	original := "---\r\ncnt:name: Person\r\nmobile: ''\r\nbirthday: ''\r\ncnt:snooze-until: 2026-12-01\r\n---\r\nUntouched body.\r\n"
	if err := os.WriteFile(path, []byte(original), 0640); err != nil {
		t.Fatal(err)
	}
	fields, err := ImportTelegram(path, "--02-29", "+12345678900", time.Time{}, schema, true)
	if err != nil || len(fields) != 2 {
		t.Fatal(fields, err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != original {
		t.Fatal("preview wrote note")
	}
	if _, err := ImportTelegram(path, "--02-29", "+12345678900", time.Time{}, schema, false); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if !strings.Contains(string(data), "birthday: \"--02-29\"\r\n") || !strings.HasSuffix(string(data), "---\r\nUntouched body.\r\n") || !strings.Contains(string(data), "cnt:snooze-until: 2026-12-01\r\n") {
		t.Fatal(string(data))
	}
	previous := string(data)
	fields, err = ImportTelegram(path, "1990-01-01", "+99999999999", time.Time{}, schema, false)
	if err != nil || len(fields) != 0 {
		t.Fatal(fields, err)
	}
	data, _ = os.ReadFile(path)
	if string(data) != previous {
		t.Fatal("overwrote existing profile")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0640 {
		t.Fatal(info.Mode())
	}
}
