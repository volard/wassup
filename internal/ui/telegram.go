package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"wassup/internal/contacts"
	telegramsync "wassup/internal/telegram"
)

type TelegramService interface {
	Snapshot() (map[string]telegramsync.LinkStatus, error)
	Check(context.Context, []contacts.Contact) (map[string]telegramsync.LinkStatus, error)
	Resolve(context.Context, contacts.Contact, string, bool) (telegramsync.Candidate, error)
	Save(context.Context, contacts.Contact, telegramsync.Candidate) (telegramsync.LinkStatus, error)
}

type telegramResult struct {
	request   uint64
	kind      string
	target    contacts.Contact
	statuses  map[string]telegramsync.LinkStatus
	candidate telegramsync.Candidate
	status    telegramsync.LinkStatus
	err       error
}

func (m Model) WithTelegram(service TelegramService, interval ...int) Model {
	m.telegramCheckDays = 14
	if len(interval) > 0 {
		m.telegramCheckDays = interval[0]
	}
	m.telegram = service
	m.telegramLinks = map[string]telegramsync.LinkStatus{}
	m.loadTelegramSnapshot()
	if len(m.telegramLinks) > 0 {
		updated, command := m.checkTelegram(m.contacts, false)
		updated.telegramInit = command
		return updated
	}
	return m
}

func (m *Model) loadTelegramSnapshot() {
	if m.telegram == nil {
		return
	}
	snapshot, err := m.telegram.Snapshot()
	if err != nil {
		m.setError(err)
		return
	}
	m.telegramLinks = snapshot
}

func (m *Model) telegramCommand(kind string, target contacts.Contact, work func(context.Context) telegramResult) tea.Cmd {
	if m.telegramCancel != nil {
		m.telegramCancel()
	}
	budget := 60 * time.Second
	if kind == "check" {
		budget = 2*time.Minute + time.Duration(len(m.telegramLinks))*21*time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	m.telegramCancel = cancel
	m.telegramRequest++
	request := m.telegramRequest
	m.telegramBusy = true
	m.telegramKind = kind
	m.telegramStarted = time.Now()
	m.telegramElapsed = 0
	m.telegramPhase = "Starting"
	m.telegramDone = 0
	m.telegramProgress = make(chan telegramsync.Progress, 1)
	progress := m.telegramProgress
	ctx = telegramsync.WithProgress(ctx, func(p telegramsync.Progress) {
		select {
		case progress <- p:
		default:
			select {
			case <-progress:
			default:
			}
			select {
			case progress <- p:
			default:
			}
		}
	})
	workCommand := func() tea.Msg {
		defer cancel()
		result := work(ctx)
		result.request = request
		result.kind = kind
		result.target = target
		return result
	}
	return tea.Batch(workCommand, telegramPulse(request))
}

func (m Model) checkTelegram(notes []contacts.Contact, force bool) (Model, tea.Cmd) {
	if m.telegram == nil || m.telegramBusy {
		return m, nil
	}
	var linked []contacts.Contact
	for _, note := range notes {
		if status, ok := m.telegramLinks[note.Path]; ok && (force || status.Due(time.Now(), m.telegramCheckDays)) {
			linked = append(linked, note)
		}
	}
	if len(linked) == 0 {
		if force {
			m.status, m.statusErr = "No linked Telegram contacts to check", false
		}
		return m, nil
	}
	m.telegramTotal = len(linked)
	m.status, m.statusErr = "", false
	service := m.telegram
	command := m.telegramCommand("check", contacts.Contact{}, func(ctx context.Context) telegramResult {
		statuses, err := service.Check(ctx, linked)
		return telegramResult{statuses: statuses, err: err}
	})
	return m, command
}

func (m Model) editTelegram() (tea.Model, tea.Cmd) {
	if m.telegram == nil {
		m.status, m.statusErr = "Telegram is not configured for this view", true
		return m, nil
	}
	if m.telegramBusy {
		m.cancelTelegram()
	}
	visible := m.visible()
	if len(visible) == 0 {
		return m, nil
	}
	m.telegramTarget = visible[m.cursor]
	m.inputValue = telegramsync.PreferredReference(m.telegramTarget, m.telegramLinks[m.telegramTarget.Path])
	m.telegramOriginal = m.inputValue
	m.input = inputTelegram
	m.status, m.statusErr = "", false
	return m, nil
}

func (m Model) resolveTelegram() (tea.Model, tea.Cmd) {
	if strings.TrimSpace(m.inputValue) == "" {
		m.status, m.statusErr = "Enter a Telegram username or phone number", true
		return m, nil
	}
	target, value, service := m.telegramTarget, m.inputValue, m.telegram
	keepID := value == m.telegramOriginal && m.telegramLinks[target.Path].Person.ID != 0
	m.input = inputTelegramWaiting
	m.status, m.statusErr = "Looking up Telegram account…", false
	command := m.telegramCommand("resolve", target, func(ctx context.Context) telegramResult {
		candidate, err := service.Resolve(ctx, target, value, keepID)
		return telegramResult{candidate: candidate, err: err}
	})
	return m, command
}

func (m Model) saveTelegram() (tea.Model, tea.Cmd) {
	target, candidate, service := m.telegramTarget, m.telegramCandidate, m.telegram
	m.input = inputTelegramWaiting
	m.status, m.statusErr = "Saving Telegram link…", false
	command := m.telegramCommand("save", target, func(ctx context.Context) telegramResult {
		status, err := service.Save(ctx, target, candidate)
		return telegramResult{status: status, err: err}
	})
	return m, command
}

func (m Model) telegramFinished(msg telegramResult) (tea.Model, tea.Cmd) {
	if msg.request != m.telegramRequest {
		return m, nil
	}
	m.telegramBusy = false
	m.telegramCancel = nil
	if msg.kind == "check" {
		m.reload()
		checked := 0
		for path, status := range msg.statuses {
			m.telegramLinks[path] = status
			if status.LastError == "" && !status.LastAttemptAt.Before(m.telegramStarted) {
				checked++
			}
		}
		if msg.err != nil {
			m.status, m.statusErr = fmt.Sprintf("Telegram check stopped: %d/%d checked · %s · V retry all", checked, m.telegramTotal, friendlyTelegramError(msg.err)), true
		} else {
			m.status, m.statusErr = fmt.Sprintf("Telegram check complete: %d/%d checked; missing birthdays/phones imported in %s", checked, m.telegramTotal, time.Since(m.telegramStarted).Round(time.Second)), false
		}
		return m, nil
	}
	if msg.err != nil {
		m.input = inputTelegram
		m.status, m.statusErr = friendlyTelegramError(msg.err), true
		return m, nil
	}
	if msg.kind == "resolve" {
		m.telegramCandidate = msg.candidate
		m.input = inputTelegramConfirm
		m.status, m.statusErr = "", false
		return m, nil
	}
	m.telegramLinks[msg.target.Path] = msg.status
	m.input, m.inputValue = inputNone, ""
	m.reload()
	m.status, m.statusErr = "Telegram linked to "+msg.target.Name, false
	return m, nil
}

func (m Model) telegramBadge(path string) string {
	status, ok := m.telegramLinks[path]
	if !ok {
		return "   "
	}
	if status.LastError != "" {
		return dimStyle.Render("✈? ")
	}
	switch status.State {
	case telegramsync.LinkValid:
		return successStyle.Render("✈✓ ")
	case telegramsync.LinkChanged:
		return todayStyle.Render("✈~ ")
	case telegramsync.LinkInvalid:
		return errorStyle.Render("✈! ")
	default:
		return dimStyle.Render("✈? ")
	}
}

func (m Model) telegramDetails(contact contacts.Contact) string {
	status, ok := m.telegramLinks[contact.Path]
	if !ok {
		return ""
	}
	person := status.Person
	reference := telegramsync.PreferredReference(contacts.Contact{}, status)
	detail := fmt.Sprintf("Telegram: %s %s\n%s", person.Name, reference, status.Detail)
	if !status.CheckedAt.IsZero() {
		detail += "\nLast checked " + status.CheckedAt.Local().Format("02 Jan 15:04")
	}
	if status.LastError != "" {
		detail += "\nLast attempt failed: " + friendlyTelegramError(errors.New(status.LastError))
	}
	if m.telegramCheckDays == 0 {
		detail += "\nAutomatic checks off · v check now"
	} else {
		last := status.LastAttemptAt
		if last.IsZero() {
			last = status.CheckedAt
		}
		if !last.IsZero() {
			detail += "\nNext automatic check " + last.AddDate(0, 0, m.telegramCheckDays).Local().Format("02 Jan 2006")
		}
	}
	return detail
}

func (m Model) telegramPrompt() string {
	switch m.input {
	case inputTelegram:
		return "Telegram for " + m.telegramTarget.Name + ": " + m.inputValue + "█\n" + dimStyle.Render("username or +phone · ctrl+u clear · enter look up · esc cancel")
	case inputTelegramConfirm:
		p := m.telegramCandidate.Person
		reference := m.telegramCandidate.Reference
		if reference == "" {
			reference = fmt.Sprintf("ID %d", p.ID)
		}
		if p.Phone != "" && !m.telegramCandidate.IsPhone {
			reference += " · +" + strings.TrimPrefix(p.Phone, "+")
		}
		text := fmt.Sprintf("Link %s → %s (%s)?", m.telegramTarget.Name, p.Name, reference)
		return text + "\n" + dimStyle.Render("enter save link and note details · esc cancel")
	default:
		return dimStyle.Render("Telegram request in progress · esc cancel")
	}
}

type telegramTick struct {
	request uint64
	now     time.Time
}

func telegramPulse(request uint64) tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(now time.Time) tea.Msg { return telegramTick{request, now} })
}
func (m Model) telegramTicked(tick telegramTick) (tea.Model, tea.Cmd) {
	if !m.telegramBusy || tick.request != m.telegramRequest {
		return m, nil
	}
	m.telegramElapsed = tick.now.Sub(m.telegramStarted)
	for {
		select {
		case p := <-m.telegramProgress:
			m.telegramPhase = p.Phase
			if p.Total > 0 {
				m.telegramDone, m.telegramTotal = p.Done, p.Total
			}
		default:
			return m, telegramPulse(m.telegramRequest)
		}
	}
}
func (m *Model) cancelTelegram() {
	if m.telegramCancel != nil {
		m.telegramCancel()
	}
	m.telegramRequest++
	m.telegramBusy = false
	m.telegramCancel = nil
	m.status, m.statusErr = "Telegram check canceled; cached results kept · V retry all", false
}
func (m Model) telegramActivity() string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	frame := frames[int(m.telegramElapsed/(200*time.Millisecond))%len(frames)]
	count := ""
	if m.telegramKind == "check" {
		count = fmt.Sprintf(" %d/%d contacts ·", m.telegramDone, m.telegramTotal)
	}
	return fmt.Sprintf("%s Telegram%s %s · %ds · esc cancel", frame, count, m.telegramPhase, int(m.telegramElapsed.Seconds()))
}
func friendlyTelegramError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "context deadline exceeded") {
		return "Telegram timed out; cached results kept"
	}
	if errors.Is(err, context.Canceled) {
		return "Telegram request canceled"
	}
	return err.Error()
}
