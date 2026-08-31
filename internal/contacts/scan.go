package contacts

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

var ErrNoFrontmatter = errors.New("note has no YAML frontmatter")

func Scan(dir string, schema Schema) ([]Contact, []error) {
	if err := schema.Validate(); err != nil {
		return nil, []error{err}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, []error{err}
	}

	var found []Contact
	var warnings []error
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		contact, tracked, err := read(path, schema)
		if err != nil {
			warnings = append(warnings, fmt.Errorf("%s: %w", entry.Name(), err))
			continue
		}
		if tracked {
			found = append(found, contact)
		}
	}

	sort.Slice(found, func(i, j int) bool {
		return strings.ToLower(found[i].Name) < strings.ToLower(found[j].Name)
	})
	return found, warnings
}

func read(path string, schema Schema) (Contact, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Contact{}, false, err
	}
	frontmatter, body, _, err := splitFrontmatter(data)
	if err != nil {
		return Contact{}, false, err
	}
	if err := validateMetadata(frontmatter, schema); err != nil {
		return Contact{}, false, err
	}
	everyValue, everyExists := fieldValue(frontmatter, schema.ContactEveryDays)
	everyDays, err := strconv.Atoi(everyValue)
	tracked := false
	if everyExists {
		if err != nil || everyDays <= 0 {
			return Contact{}, false, fmt.Errorf("%s must be a positive integer", schema.ContactEveryDays)
		}
		tracked = true
	}

	lastValue, _ := fieldValue(frontmatter, schema.LastContact)
	last, err := parseOptionalDate(lastValue)
	if err != nil {
		return Contact{}, false, fmt.Errorf("%s: %w", schema.LastContact, err)
	}
	snoozeValue, _ := fieldValue(frontmatter, schema.SnoozeUntil)
	snooze, err := parseOptionalDate(snoozeValue)
	if err != nil {
		return Contact{}, false, fmt.Errorf("%s: %w", schema.SnoozeUntil, err)
	}

	displayName, _ := fieldValue(frontmatter, schema.DisplayName)
	fallbackName, _ := fieldValue(frontmatter, schema.FallbackName)
	name := firstNonempty(displayName, heading(body), fallbackName, strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	return Contact{
		Path:        path,
		Name:        name,
		Tracked:     tracked,
		LastContact: last,
		EveryDays:   everyDays,
		SnoozeUntil: snooze,
		Note:        previewText(frontmatter, body, schema),
	}, true, nil
}

func previewText(frontmatter, body []byte, schema Schema) string {
	keys := []string{
		schema.DisplayName,
		schema.BirthDate,
		schema.RelationshipOrigin,
		schema.RelationshipContext,
		schema.Photo,
		schema.Phone,
		schema.Email,
		schema.Telegram,
	}
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		if value, exists := fieldValue(frontmatter, key); exists && value != "" {
			lines = append(lines, key+": "+value)
		}
	}
	content := strings.TrimSpace(string(body))
	if content != "" && len(lines) > 0 {
		return strings.Join(lines, "\n") + "\n\n" + content
	}
	if content != "" {
		return content
	}
	return strings.Join(lines, "\n")
}

func validateMetadata(frontmatter []byte, schema Schema) error {
	allowed := map[string]bool{
		schema.DisplayName:         true,
		schema.FallbackName:        true,
		schema.BirthDate:           true,
		schema.RelationshipOrigin:  true,
		schema.RelationshipContext: true,
		schema.Photo:               true,
		schema.Phone:               true,
		schema.Email:               true,
		schema.Telegram:            true,
		schema.LastContact:         true,
		schema.ContactEveryDays:    true,
		schema.SnoozeUntil:         true,
	}
	seen := make(map[string]bool, len(allowed))
	for line := range strings.Lines(string(frontmatter)) {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "cnt:") && !strings.HasPrefix(line, "rel:") {
			continue
		}
		separator := strings.Index(line[4:], ":")
		if separator < 0 {
			return fmt.Errorf("invalid namespaced frontmatter line %q", line)
		}
		key := line[:4+separator]
		if !allowed[key] {
			return fmt.Errorf("unknown contact field %q", key)
		}
		if seen[key] {
			return fmt.Errorf("duplicate contact field %q", key)
		}
		seen[key] = true
	}
	if value, exists := fieldValue(frontmatter, schema.BirthDate); exists && value != "" {
		layout := dateLayout
		if strings.HasPrefix(value, "--") {
			layout = "--01-02"
		}
		if _, err := time.Parse(layout, value); err != nil {
			return fmt.Errorf("%s must be YYYY-MM-DD or --MM-DD; got %q", schema.BirthDate, value)
		}
	}
	if value, exists := fieldValue(frontmatter, schema.RelationshipOrigin); exists {
		for _, allowed := range schema.RelationshipOrigins {
			if value == allowed {
				return nil
			}
		}
		return fmt.Errorf("%s must be one of %s; got %q", schema.RelationshipOrigin, strings.Join(schema.RelationshipOrigins, ", "), value)
	}
	return nil
}

func parseOptionalDate(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.ParseInLocation(dateLayout, value, time.Local)
	if err != nil {
		return nil, fmt.Errorf("expected YYYY-MM-DD, got %q", value)
	}
	return &parsed, nil
}

func fieldValue(frontmatter []byte, key string) (string, bool) {
	prefix := key + ":"
	for line := range strings.Lines(string(frontmatter)) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return unquote(strings.TrimSpace(strings.TrimPrefix(line, prefix))), true
		}
	}
	return "", false
}

func unquote(value string) string {
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return strings.ReplaceAll(value[1:len(value)-1], "''", "'")
	}
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		if decoded, err := strconv.Unquote(value); err == nil {
			return decoded
		}
	}
	return value
}

func heading(body []byte) string {
	for line := range strings.Lines(string(body)) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "Unknown"
}

type frontmatterParts struct {
	start       int
	contentFrom int
	contentTo   int
	end         int
	newline     string
}

func splitFrontmatter(data []byte) ([]byte, []byte, frontmatterParts, error) {
	newline := "\n"
	if bytes.Contains(data, []byte("\r\n")) {
		newline = "\r\n"
	}
	marker := []byte("---" + newline)
	if !bytes.HasPrefix(data, marker) {
		return nil, nil, frontmatterParts{}, ErrNoFrontmatter
	}
	contentFrom := len(marker)
	closing := bytes.Index(data[contentFrom:], []byte(newline+"---"+newline))
	if closing < 0 {
		return nil, nil, frontmatterParts{}, fmt.Errorf("unterminated YAML frontmatter")
	}
	contentTo := contentFrom + closing
	end := contentTo + len(newline+"---"+newline)
	parts := frontmatterParts{start: 0, contentFrom: contentFrom, contentTo: contentTo, end: end, newline: newline}
	return data[contentFrom:contentTo], data[end:], parts, nil
}
