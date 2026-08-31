package notifications

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"wassup/internal/contacts"
)

type Digest struct {
	Title       string
	Body        string
	DueCount    int
	Suggestions []contacts.Contact
}

func BuildDigest(all []contacts.Contact, today time.Time, maxSuggestions int) (Digest, bool) {
	due := make([]contacts.Contact, 0, len(all))
	for _, contact := range all {
		if contact.Tracked && contact.DaysUntil(today) <= 0 {
			due = append(due, contact)
		}
	}
	if len(due) == 0 {
		return Digest{}, false
	}
	sort.SliceStable(due, func(i, j int) bool {
		left, right := due[i].DueDate(today), due[j].DueDate(today)
		if left.Equal(right) {
			return strings.ToLower(due[i].Name) < strings.ToLower(due[j].Name)
		}
		return left.Before(right)
	})
	maxSuggestions = min(max(1, maxSuggestions), len(due))
	suggestions := append([]contacts.Contact(nil), due[:maxSuggestions]...)
	noun := "contacts"
	if len(due) == 1 {
		noun = "contact"
	}
	names := make([]string, len(suggestions))
	for index, contact := range suggestions {
		names[index] = contact.Name
	}
	label := "Today's suggestion: "
	if len(names) > 1 {
		label = "Today's suggestions: "
	}
	return Digest{
		Title:       fmt.Sprintf("Wassup: %d %s due", len(due), noun),
		Body:        label + strings.Join(names, ", "),
		DueCount:    len(due),
		Suggestions: suggestions,
	}, true
}

func SendOnce(command []string, digest Digest, statePath string, today time.Time) (bool, error) {
	date := today.Format("2006-01-02")
	previous, err := os.ReadFile(statePath)
	if err == nil && strings.TrimSpace(string(previous)) == date {
		return false, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("read notification state: %w", err)
	}
	if err := Send(command, digest.Title, digest.Body); err != nil {
		return false, err
	}
	if err := atomicWrite(statePath, []byte(date+"\n"), 0o600); err != nil {
		return false, fmt.Errorf("write notification state: %w", err)
	}
	return true, nil
}

func Send(command []string, title, body string) error {
	if len(command) == 0 {
		return errors.New("notification command is empty")
	}
	arguments := make([]string, len(command))
	for index, argument := range command {
		argument = strings.ReplaceAll(argument, "{title}", title)
		argument = strings.ReplaceAll(argument, "{body}", body)
		arguments[index] = argument
	}
	cmd := exec.Command(arguments[0], arguments[1:]...)
	if output, err := cmd.CombinedOutput(); err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return fmt.Errorf("send desktop notification: %w", err)
		}
		return fmt.Errorf("send desktop notification: %w: %s", err, message)
	}
	return nil
}

func StatePath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "notification-state")
}

func InstallSystemd(executable, configPath, dailyAt string) (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	unitDir := filepath.Join(configDir, "systemd", "user")
	service, timer := SystemdUnits(executable, configPath, dailyAt)
	if err := atomicWrite(filepath.Join(unitDir, "wassup-notify.service"), []byte(service), 0o644); err != nil {
		return "", err
	}
	if err := atomicWrite(filepath.Join(unitDir, "wassup-notify.timer"), []byte(timer), 0o644); err != nil {
		return "", err
	}
	for _, arguments := range [][]string{
		{"--user", "daemon-reload"},
		{"--user", "enable", "--now", "wassup-notify.timer"},
	} {
		command := exec.Command("systemctl", arguments...)
		if output, err := command.CombinedOutput(); err != nil {
			return "", fmt.Errorf("systemctl %s: %w: %s", strings.Join(arguments, " "), err, strings.TrimSpace(string(output)))
		}
	}
	return filepath.Join(unitDir, "wassup-notify.timer"), nil
}

func SystemdUnits(executable, configPath, dailyAt string) (string, string) {
	service := `[Unit]
Description=Wassup due-contact desktop notification
After=graphical-session.target

[Service]
Type=oneshot
ExecStart=` + systemdQuote(executable) + ` --config ` + systemdQuote(configPath) + ` --notify
`
	timer := `[Unit]
Description=Daily Wassup due-contact reminder

[Timer]
OnCalendar=*-*-* ` + dailyAt + `:00
Persistent=true
AccuracySec=1m

[Install]
WantedBy=timers.target
`
	return service, timer
}

func systemdQuote(value string) string {
	value = strings.ReplaceAll(value, "%", "%%")
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}

func atomicWrite(path string, data []byte, mode os.FileMode) (returnErr error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".wassup-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		if returnErr != nil {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
