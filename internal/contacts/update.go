package contacts

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

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
