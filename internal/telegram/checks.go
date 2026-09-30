package telegramsync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type Progress struct {
	Phase       string
	Done, Total int
}
type progressKey struct{}

func WithProgress(ctx context.Context, report func(Progress)) context.Context {
	return context.WithValue(ctx, progressKey{}, report)
}
func ReportProgress(ctx context.Context, p Progress) {
	if report, ok := ctx.Value(progressKey{}).(func(Progress)); ok {
		report(p)
	}
}

// Failed attempts also defer automatic retry, avoiding repeated network calls
// on every launch. Manual checks always bypass this interval.
func (s LinkStatus) Due(now time.Time, days int) bool {
	if days <= 0 {
		return false
	}
	last := s.LastAttemptAt
	if last.IsZero() {
		last = s.CheckedAt
	}
	return last.IsZero() || !now.Before(last.AddDate(0, 0, days))
}

type checkCache struct {
	AccountID int64                 `json:"account_id"`
	Results   map[string]LinkStatus `json:"results"`
}

func checksPath(configPath string) string { return filepath.Join(StateDir(configPath), "checks.json") }
func readChecks(configPath string, accountID int64) (checkCache, error) {
	empty := checkCache{AccountID: accountID, Results: map[string]LinkStatus{}}
	var cached checkCache
	if err := readJSON(checksPath(configPath), &cached); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return empty, nil
		}
		return empty, err
	}
	if cached.AccountID != accountID {
		return empty, nil
	}
	if cached.Results == nil {
		cached.Results = map[string]LinkStatus{}
	}
	return cached, nil
}
func (s Service) persistChecks(results map[string]LinkStatus, accountID int64) error {
	unlock, err := lockState(StateDir(s.ConfigPath))
	if err != nil {
		return err
	}
	defer unlock()
	var saved links
	if err := readJSON(filepath.Join(StateDir(s.ConfigPath), "links.json"), &saved); err != nil {
		return err
	}
	if saved.AccountID != accountID {
		return errors.New("Telegram account changed; check results were not cached")
	}
	cache, err := readChecks(s.ConfigPath, saved.AccountID)
	if err != nil {
		return err
	}
	for path, status := range results {
		if current, ok := saved.Notes[path]; ok && current.ID == status.Person.ID {
			// Do not replace results from a newer concurrent operation.
			if old := cache.Results[path]; old.LastAttemptAt.After(status.LastAttemptAt) {
				continue
			}
			cache.Results[path] = status
		}
	}
	return writeJSON(checksPath(s.ConfigPath), cache)
}
