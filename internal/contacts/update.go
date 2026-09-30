package contacts

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SetTelegramIdentity records explicitly confirmed Telegram details, preserving
// scheduling fields and leaving phone numbers unchanged when Telegram hides them.
func SetTelegramIdentity(path, handle, number string, schema Schema) error {
	if err := schema.Validate(); err != nil {
		return err
	}
	if _, _, err := read(path, schema); err != nil {
		return err
	}
	if strings.ContainsAny(handle+number, "\r\n") {
		return fmt.Errorf("Telegram details cannot contain newlines")
	}
	changes := map[string]*string{schema.Telegram: nil}
	if handle != "" {
		value := strconv.Quote("@" + strings.TrimPrefix(handle, "@"))
		changes[schema.Telegram] = &value
	}
	if number != "" {
		value := strconv.Quote(number)
		changes[schema.Phone] = &value
	}
	return updateFields(path, changes)
}

func MarkContacted(path string, today time.Time, schema Schema) error {
	if err := schema.Validate(); err != nil {
		return err
	}
	value := startOfDay(today).Format(dateLayout)
	return updateFields(path, map[string]*string{
		schema.LastContact: &value,
		schema.SnoozeUntil: nil,
	})
}

// AdvanceLastContact imports a date without clearing snoozes or moving a newer
// local date backwards. Re-read the note because a sync may take some time.
func AdvanceLastContact(path string, date time.Time, schema Schema) (bool, error) {
	if err := schema.Validate(); err != nil {
		return false, err
	}
	current, _, err := read(path, schema)
	if err != nil {
		return false, err
	}
	value := date.In(time.Local).Format(dateLayout)
	if current.LastContact != nil && current.LastContact.Format(dateLayout) >= value {
		return false, nil
	}
	if err := updateFields(path, map[string]*string{schema.LastContact: &value}); err != nil {
		return false, err
	}
	return true, nil
}

func Snooze(path string, today time.Time, days int, schema Schema) error {
	if err := schema.Validate(); err != nil {
		return err
	}
	if days <= 0 {
		return fmt.Errorf("snooze days must be positive")
	}
	value := startOfDay(today).AddDate(0, 0, days).Format(dateLayout)
	return updateFields(path, map[string]*string{schema.SnoozeUntil: &value})
}

func Schedule(path string, days int, schema Schema) error {
	if err := schema.Validate(); err != nil {
		return err
	}
	if days <= 0 {
		return fmt.Errorf("contact interval must be positive")
	}
	value := fmt.Sprintf("%d", days)
	return updateFields(path, map[string]*string{schema.ContactEveryDays: &value})
}

func RemoveFromKeepInTouch(path string, schema Schema) error {
	if err := schema.Validate(); err != nil {
		return err
	}
	return updateFields(path, map[string]*string{
		schema.ContactEveryDays: nil,
		schema.SnoozeUntil:      nil,
	})
}

func updateFields(path string, changes map[string]*string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	frontmatter, _, parts, err := splitFrontmatter(data)
	if err != nil {
		return err
	}

	lines := strings.Split(string(frontmatter), parts.newline)
	remove := make(map[int]bool)
	remaining := make(map[string]*string, len(changes))
	for key, value := range changes {
		remaining[key] = value
	}
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		for key, value := range remaining {
			if strings.HasPrefix(trimmed, key+":") {
				if value == nil {
					remove[index] = true
				} else {
					indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
					lines[index] = indent + key + ": " + *value
				}
				delete(remaining, key)
				break
			}
		}
	}

	keys := make([]string, 0, len(remaining))
	for key, value := range remaining {
		if value != nil {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		lines = append(lines, key+": "+*remaining[key])
	}

	updatedLines := make([]string, 0, len(lines))
	for index, line := range lines {
		if !remove[index] {
			updatedLines = append(updatedLines, line)
		}
	}
	updatedFrontmatter := []byte(strings.Join(updatedLines, parts.newline))

	var result bytes.Buffer
	result.Grow(len(data) + 64)
	result.Write(data[:parts.contentFrom])
	result.Write(updatedFrontmatter)
	result.Write(data[parts.contentTo:])
	return atomicWrite(path, result.Bytes())
}

func atomicWrite(path string, data []byte) (returnErr error) {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".wassup-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() {
		_ = temp.Close()
		if returnErr != nil {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(info.Mode()); err != nil {
		return err
	}
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	return nil
}
