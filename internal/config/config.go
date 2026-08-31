package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"wassup/internal/contacts"
)

type Config struct {
	ContactsDir   string          `json:"contacts_dir"`
	StartView     string          `json:"start_view"`
	Opener        Opener          `json:"opener"`
	Notifications Notifications   `json:"notifications"`
	Fields        contacts.Schema `json:"frontmatter_fields"`
}

type Opener struct {
	Command []string `json:"command"`
}

type Notifications struct {
	DailyAt        string   `json:"daily_at"`
	MaxSuggestions int      `json:"max_suggestions"`
	Command        []string `json:"command"`
}

const DefaultStartView = "keep_in_touch"

func Default(contactsDir string) Config {
	return Config{
		ContactsDir: contactsDir,
		StartView:   DefaultStartView,
		Opener:      Opener{Command: []string{"nvim", "{path}"}},
		Notifications: Notifications{
			DailyAt:        "18:00",
			MaxSuggestions: 1,
			Command:        []string{"notify-send", "--app-name=wassup", "--urgency=normal", "{title}", "{body}"},
		},
		Fields: contacts.DefaultSchema(),
	}
}

func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "wassup", "config.json"), nil
}

func LoadOrCreate(path string, defaults Config) (Config, bool, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return Config{}, false, err
	}
	loaded, err := load(absPath)
	if err == nil {
		return loaded, false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Config{}, false, err
	}
	if err := defaults.Validate(); err != nil {
		return Config{}, false, err
	}
	if err := writeNew(absPath, defaults); err != nil {
		if errors.Is(err, os.ErrExist) {
			loaded, loadErr := load(absPath)
			return loaded, false, loadErr
		}
		return Config{}, false, err
	}
	return defaults, true, nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.ContactsDir) == "" {
		return errors.New("contacts_dir cannot be empty")
	}
	validViews := map[string]bool{"due": true, "upcoming": true, "keep_in_touch": true, "all": true}
	if !validViews[c.StartView] {
		return fmt.Errorf("start_view must be one of due, upcoming, keep_in_touch, or all; got %q", c.StartView)
	}
	if len(c.Opener.Command) == 0 || strings.TrimSpace(c.Opener.Command[0]) == "" {
		return errors.New("opener.command must contain an executable")
	}
	hasPath := false
	for _, argument := range c.Opener.Command {
		if strings.ContainsRune(argument, 0) {
			return errors.New("opener.command cannot contain NUL characters")
		}
		hasPath = hasPath || strings.Contains(argument, "{path}")
	}
	if !hasPath {
		return errors.New("opener.command must contain a {path} placeholder")
	}
	if _, err := time.Parse("15:04", c.Notifications.DailyAt); err != nil {
		return fmt.Errorf("notifications.daily_at must be HH:MM; got %q", c.Notifications.DailyAt)
	}
	if c.Notifications.MaxSuggestions < 1 || c.Notifications.MaxSuggestions > 10 {
		return errors.New("notifications.max_suggestions must be from 1 to 10")
	}
	if len(c.Notifications.Command) == 0 || strings.TrimSpace(c.Notifications.Command[0]) == "" {
		return errors.New("notifications.command must contain an executable")
	}
	hasTitle, hasBody := false, false
	for _, argument := range c.Notifications.Command {
		if strings.ContainsRune(argument, 0) {
			return errors.New("notifications.command cannot contain NUL characters")
		}
		hasTitle = hasTitle || strings.Contains(argument, "{title}")
		hasBody = hasBody || strings.Contains(argument, "{body}")
	}
	if !hasTitle || !hasBody {
		return errors.New("notifications.command must contain {title} and {body} placeholders")
	}
	if err := c.Fields.Validate(); err != nil {
		return fmt.Errorf("frontmatter_fields: %w", err)
	}
	return nil
}

func (c Config) ResolveContactsDir(configPath string) (string, error) {
	dir := c.ContactsDir
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(filepath.Dir(configPath), dir)
	}
	return filepath.Abs(dir)
}

func load(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer file.Close()

	var result Config
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	// Configs created before start_view existed retain the new default.
	if result.StartView == "" {
		result.StartView = DefaultStartView
	}
	if len(result.Opener.Command) == 0 {
		result.Opener = Opener{Command: []string{"nvim", "{path}"}}
	}
	if result.Notifications.DailyAt == "" {
		result.Notifications = Default("").Notifications
	}
	result.Fields = withMetadataDefaults(result.Fields)
	if err := ensureEOF(decoder); err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := result.Validate(); err != nil {
		return Config{}, fmt.Errorf("invalid config %s: %w", path, err)
	}
	return result, nil
}

// withMetadataDefaults upgrades configurations created before contact metadata
// validation was added without changing customized scheduling field names.
func withMetadataDefaults(schema contacts.Schema) contacts.Schema {
	defaults := contacts.DefaultSchema()
	if schema.BirthDate == "" {
		schema.BirthDate = defaults.BirthDate
	}
	if schema.RelationshipOrigin == "" {
		schema.RelationshipOrigin = defaults.RelationshipOrigin
	}
	if schema.RelationshipContext == "" {
		schema.RelationshipContext = defaults.RelationshipContext
	}
	if schema.Photo == "" {
		schema.Photo = defaults.Photo
	}
	if schema.Phone == "" {
		schema.Phone = defaults.Phone
	}
	if schema.Email == "" {
		schema.Email = defaults.Email
	}
	if schema.Telegram == "" {
		schema.Telegram = defaults.Telegram
	}
	if len(schema.RelationshipOrigins) == 0 {
		schema.RelationshipOrigins = defaults.RelationshipOrigins
	}
	return schema
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("config contains multiple JSON values")
}

func writeNew(path string, value Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	success := false
	defer func() {
		_ = file.Close()
		if !success {
			_ = os.Remove(path)
		}
	}()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	success = true
	return nil
}
