package contacts

import (
	"fmt"
	"strings"
	"time"
)

const dateLayout = "2006-01-02"

type Schema struct {
	DisplayName         string   `json:"display_name"`
	FallbackName        string   `json:"fallback_name"`
	BirthDate           string   `json:"birth_date"`
	RelationshipOrigin  string   `json:"relationship_origin"`
	RelationshipContext string   `json:"relationship_context"`
	Photo               string   `json:"photo"`
	Phone               string   `json:"phone"`
	Email               string   `json:"email"`
	Telegram            string   `json:"telegram"`
	LastContact         string   `json:"last_contact"`
	ContactEveryDays    string   `json:"contact_every_days"`
	SnoozeUntil         string   `json:"snooze_until"`
	RelationshipOrigins []string `json:"relationship_origins"`
}

func DefaultSchema() Schema {
	return Schema{
		DisplayName:         "cnt:name",
		FallbackName:        "name",
		BirthDate:           "cnt:birth-date",
		RelationshipOrigin:  "rel:origin",
		RelationshipContext: "rel:context",
		Photo:               "cnt:photo",
		Phone:               "cnt:phone",
		Email:               "cnt:email",
		Telegram:            "cnt:telegram",
		LastContact:         "cnt:last-contact",
		ContactEveryDays:    "cnt:contact-every-days",
		SnoozeUntil:         "cnt:snooze-until",
		RelationshipOrigins: []string{"family", "friend", "school", "university", "work"},
	}
}

func (s Schema) Validate() error {
	fields := map[string]string{
		"display_name":         s.DisplayName,
		"fallback_name":        s.FallbackName,
		"birth_date":           s.BirthDate,
		"relationship_origin":  s.RelationshipOrigin,
		"relationship_context": s.RelationshipContext,
		"photo":                s.Photo,
		"phone":                s.Phone,
		"email":                s.Email,
		"telegram":             s.Telegram,
		"last_contact":         s.LastContact,
		"contact_every_days":   s.ContactEveryDays,
		"snooze_until":         s.SnoozeUntil,
	}
	seen := make(map[string]string, len(fields))
	for label, key := range fields {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("frontmatter field %q cannot be empty", label)
		}
		if key != strings.TrimSpace(key) || strings.ContainsAny(key, "\r\n") || key == "---" {
			return fmt.Errorf("frontmatter field %q has an invalid key %q", label, key)
		}
		if previous, exists := seen[key]; exists {
			return fmt.Errorf("frontmatter fields %q and %q use the same key %q", previous, label, key)
		}
		seen[key] = label
	}
	if len(s.RelationshipOrigins) == 0 {
		return fmt.Errorf("relationship_origins cannot be empty")
	}
	seenOrigins := make(map[string]bool, len(s.RelationshipOrigins))
	for _, origin := range s.RelationshipOrigins {
		if origin == "" || origin != strings.TrimSpace(origin) || strings.ToLower(origin) != origin || strings.ContainsAny(origin, " \t\r\n") {
			return fmt.Errorf("invalid relationship origin %q; use a lowercase identifier without spaces", origin)
		}
		if seenOrigins[origin] {
			return fmt.Errorf("duplicate relationship origin %q", origin)
		}
		seenOrigins[origin] = true
	}
	return nil
}

type Contact struct {
	Path        string
	Name        string
	Tracked     bool
	LastContact *time.Time
	EveryDays   int
	SnoozeUntil *time.Time
	Note        string
}

func (c Contact) DueDate(today time.Time) time.Time {
	due := startOfDay(today)
	if c.LastContact != nil {
		due = startOfDay(*c.LastContact).AddDate(0, 0, c.EveryDays)
	}
	if c.SnoozeUntil != nil && c.SnoozeUntil.After(due) {
		due = startOfDay(*c.SnoozeUntil)
	}
	return due
}

func (c Contact) DaysUntil(today time.Time) int {
	return int(c.DueDate(today).Sub(startOfDay(today)).Hours() / 24)
}

func startOfDay(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, t.Location())
}
