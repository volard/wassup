package contacts

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ImportTelegram fills missing profile fields and advances last-contact, using
// current disk values so edits made during a network request are preserved.
// A zero date leaves last-contact untouched. Preview never writes the note.
func ImportTelegram(path, birthday, phone string, date time.Time, schema Schema, preview bool) ([]string, error) {
	if err := schema.Validate(); err != nil {
		return nil, err
	}
	current, _, err := read(path, schema)
	if err != nil {
		return nil, err
	}
	if strings.ContainsAny(phone, "\r\n") {
		return nil, fmt.Errorf("phone cannot contain newlines")
	}
	changes := map[string]*string{}
	var fields []string
	if birthday != "" && current.BirthDate == "" {
		layout := dateLayout
		if strings.HasPrefix(birthday, "--") {
			layout = "--01-02"
		}
		if _, err := time.Parse(layout, birthday); err != nil {
			return nil, err
		}
		value := strconv.Quote(birthday)
		changes[schema.BirthDate] = &value
		fields = append(fields, "birthday: "+birthday)
	}
	if phone != "" && current.Phone == "" {
		value := strconv.Quote(phone)
		changes[schema.Phone] = &value
		fields = append(fields, "phone added")
	}
	if !date.IsZero() {
		value := date.In(time.Local).Format(dateLayout)
		if current.LastContact == nil || current.LastContact.Format(dateLayout) < value {
			old := "unset"
			if current.LastContact != nil {
				old = current.LastContact.Format(dateLayout)
			}
			changes[schema.LastContact] = &value
			fields = append(fields, old+" → "+value)
		}
	}
	if len(changes) > 0 && !preview {
		err = updateFields(path, changes)
	}
	return fields, err
}
