package telegramsync

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
	"unicode"

	"wassup/internal/contacts"
)

type historySource interface {
	Profile(context.Context, Person) (Profile, error)
	LatestOutgoing(context.Context, Person) (time.Time, error)
}

// Collect the entire plan before writing any notes. An API failure must never
// look like a successful sync of the remaining contacts.
func syncNotes(ctx context.Context, remote historySource, found []contacts.Contact, saved links, schema contacts.Schema, dryRun bool, out io.Writer) error {
	type change struct {
		contact contacts.Contact
		date    time.Time
		profile Profile
	}
	var changes []change
	linked := 0
	for _, c := range found {
		p, ok := saved.Notes[c.Path]
		if !ok {
			continue
		}
		linked++
		if err := ctx.Err(); err != nil {
			return err
		}
		date, err := remote.LatestOutgoing(ctx, p)
		if err != nil {
			return fmt.Errorf("read Telegram history for %s: %w (no note updates applied)", c.Name, err)
		}
		profile, err := remote.Profile(ctx, p)
		if err != nil {
			return fmt.Errorf("read Telegram profile for %s: %w (no note updates applied)", c.Name, err)
		}
		if date.After(time.Now().Add(5 * time.Minute)) {
			return fmt.Errorf("unexpected future message date for %s; no note updates applied", c.Name)
		}
		changes = append(changes, change{c, date, profile})
	}

	count := 0
	for _, ch := range changes {
		if err := ctx.Err(); err != nil {
			return err
		}
		fields, err := contacts.ImportTelegram(ch.contact.Path, ch.profile.Birthday, ch.profile.Phone, ch.date, schema, dryRun)
		if err != nil {
			return fmt.Errorf("update %s after %d changes: %w", ch.contact.Name, count, err)
		}
		if len(fields) == 0 {
			continue
		}
		fmt.Fprintf(out, "%s: %s\n", ch.contact.Name, strings.Join(fields, "; "))
		count++
	}
	action := "Updated"
	if dryRun {
		action = "Would update"
	}
	fmt.Fprintf(out, "%s %d notes; %d linked, %d unlinked.\n", action, count, linked, len(found)-linked)
	if linked == 0 {
		fmt.Fprintln(out, "Run wassup telegram link to link your notes first.")
	}
	return nil
}

func username(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "t.me/") || strings.HasPrefix(value, "telegram.me/") {
		value = "https://" + value
	}
	if strings.Contains(value, "://") {
		u, err := url.Parse(value)
		if err != nil {
			return ""
		}
		switch strings.ToLower(u.Hostname()) {
		case "t.me", "telegram.me", "www.t.me":
			if u.Scheme != "https" && u.Scheme != "http" {
				return ""
			}
			value = strings.Trim(u.Path, "/")
		default:
			return ""
		}
	}
	value = strings.TrimPrefix(value, "@")
	if value == "" {
		return ""
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_') {
			return ""
		}
	}
	return strings.ToLower(value)
}

func phone(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else if !unicode.IsSpace(r) && !strings.ContainsRune("+()-.", r) {
			return ""
		}
	}
	if b.Len() < 7 {
		return ""
	}
	return b.String()
}

func suggestions(c contacts.Contact, people []Person) []Person {
	var exact, names []Person
	handle, number := username(c.Telegram), phone(c.Phone)
	for _, p := range people {
		if (handle != "" && handle == username(p.Username)) || (number != "" && number == phone(p.Phone)) {
			exact = append(exact, p)
		} else if strings.EqualFold(strings.TrimSpace(c.Name), strings.TrimSpace(p.Name)) {
			names = append(names, p)
		}
	}
	return append(exact, names...)
}
