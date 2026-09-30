package telegramsync

import (
	"context"
	"fmt"
	"time"

	"github.com/gotd/td/tg"
)

type Profile struct {
	Birthday string
	Phone    string
}

func (r *remoteClient) Profile(ctx context.Context, p Person) (Profile, error) {
	callCtx, cancel, err := r.requestContext(ctx)
	if err != nil {
		return Profile{}, err
	}
	defer cancel()
	full, err := r.api.UsersGetFullUser(callCtx, &tg.InputUser{UserID: p.ID, AccessHash: p.AccessHash})
	if err != nil {
		return Profile{}, err
	}
	if full == nil || full.FullUser.ID != p.ID {
		return Profile{}, fmt.Errorf("Telegram returned a different profile")
	}
	var profile Profile
	found := false
	for _, item := range full.Users {
		if u, ok := item.(*tg.User); ok && u.ID == p.ID {
			if _, valid := personFromUser(u); !valid {
				return Profile{}, fmt.Errorf("Telegram profile is unavailable")
			}
			found = true
			if number := phone(u.Phone); number != "" {
				profile.Phone = "+" + number
			}
		}
	}
	if !found {
		return Profile{}, fmt.Errorf("Telegram profile user is missing")
	}
	if birthday, ok := full.FullUser.GetBirthday(); ok {
		value := fmt.Sprintf("--%02d-%02d", birthday.Month, birthday.Day)
		layout := "--01-02"
		if year, visible := birthday.GetYear(); visible {
			if year < 1 || year > 9999 {
				return Profile{}, fmt.Errorf("invalid Telegram birthday year")
			}
			value = fmt.Sprintf("%04d-%02d-%02d", year, birthday.Month, birthday.Day)
			layout = "2006-01-02"
		}
		if _, err := time.Parse(layout, value); err != nil {
			return Profile{}, fmt.Errorf("invalid Telegram birthday: %w", err)
		}
		profile.Birthday = value
	}
	return profile, nil
}
