package ui

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"wassup/internal/contacts"
	telegramsync "wassup/internal/telegram"
)

type viewMode int

const (
	viewDue viewMode = iota
	viewUpcoming
	viewKeepInTouch
	viewAll
)

type inputMode int

const (
	inputNone inputMode = iota
	inputSearch
	inputSnooze
	inputSchedule
	inputContactDate
	inputTelegram
	inputTelegramConfirm
	inputTelegramWaiting
)

type Model struct {
	dir                         string
	schema                      contacts.Schema
	opener                      []string
	contacts                    []contacts.Contact
	warnings                    []error
	view                        viewMode
	input                       inputMode
	query                       string
	inputValue                  string
	cursor                      int
	width                       int
	height                      int
	preview                     bool
	status                      string
	statusErr                   bool
	today                       time.Time
	telegram                    TelegramService
	telegramLinks               map[string]telegramsync.LinkStatus
	telegramTarget              contacts.Contact
	telegramOriginal            string
	telegramCandidate           telegramsync.Candidate
	telegramRequest             uint64
	telegramBusy                bool
	telegramCancel              context.CancelFunc
	telegramInit                tea.Cmd
	telegramCheckDays           int
	telegramKind                string
	telegramStarted             time.Time
	telegramElapsed             time.Duration
	telegramPhase               string
	telegramDone, telegramTotal int
	telegramProgress            chan telegramsync.Progress
}

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	activeStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229")).Background(lipgloss.Color("62")).Padding(0, 1)
	inactiveStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Padding(0, 1)
	cursorStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	overdueStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	todayStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	soonStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("120"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	successStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("120"))
)

type editorFinishedMsg struct {
	name string
	err  error
}

func New(dir string, schema contacts.Schema, startView string, opener []string, initialStatus string) Model {
	m := Model{dir: dir, schema: schema, opener: append([]string(nil), opener...), view: viewFromName(startView), width: 80, height: 24, preview: true, today: startOfDay(time.Now()), status: initialStatus}
	m.reload()
	return m
}

func viewFromName(name string) viewMode {
	switch name {
	case "upcoming":
		return viewUpcoming
	case "keep_in_touch":
		return viewKeepInTouch
	case "all":
		return viewAll
	default:
		return viewDue
	}
}

func (m Model) Init() tea.Cmd { return m.telegramInit }

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case telegramTick:
		return m.telegramTicked(msg)
	case telegramResult:
		return m.telegramFinished(msg)
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			if m.telegramCancel != nil {
				m.telegramCancel()
			}
			return m, tea.Quit
		}
		if m.input != inputNone {
			return m.updateInput(msg)
		}
		return m.updateNormal(msg)
	case editorFinishedMsg:
		m.reload()
		if msg.err != nil {
			m.status, m.statusErr = "Could not open "+msg.name+": "+msg.err.Error(), true
		} else {
			m.status, m.statusErr = "Returned from "+msg.name, false
		}
		return m, nil
	}
	return m, nil
}

func (m Model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.visible()
	switch msg.String() {
	case "q":
		if m.telegramCancel != nil {
			m.telegramCancel()
		}
		return m, tea.Quit
	case "t":
		return m.editTelegram()
	case "v":
		if len(visible) > 0 && !m.telegramBusy {
			return m.checkTelegram([]contacts.Contact{visible[m.cursor]}, true)
		}
	case "V":
		return m.checkTelegram(m.contacts, true)
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor+1 < len(visible) {
			m.cursor++
		}
	case "tab", "l", "]":
		m.view = (m.view + 1) % 4
		m.cursor = 0
	case "shift+tab", "h", "[":
		m.view = (m.view + 3) % 4
		m.cursor = 0
	case "/":
		m.input = inputSearch
		m.preview = false
	case "s":
		if len(visible) > 0 && visible[m.cursor].Tracked {
			m.input, m.inputValue = inputSnooze, ""
			m.preview = false
		} else if len(visible) > 0 {
			m.status, m.statusErr = "Schedule this contact first with a", true
		}
	case "a":
		if len(visible) > 0 {
			m.input, m.inputValue = inputSchedule, ""
			if visible[m.cursor].Tracked {
				m.inputValue = strconv.Itoa(visible[m.cursor].EveryDays)
			}
			m.preview = false
		}
	case "d":
		if len(visible) > 0 && visible[m.cursor].Tracked {
			m.input, m.inputValue = inputContactDate, ""
			m.preview = false
		} else if len(visible) > 0 {
			m.status, m.statusErr = "Schedule this contact first with a", true
		}
	case "x":
		if len(visible) > 0 && visible[m.cursor].Tracked {
			selected := visible[m.cursor]
			if err := contacts.RemoveFromKeepInTouch(selected.Path, m.schema); err != nil {
				m.setError(err)
			} else {
				m.status, m.statusErr = "Removed "+selected.Name+" from Keep in touch", false
				m.reload()
			}
		}
	case "enter":
		if len(visible) > 0 {
			m.preview = !m.preview
		}
	case "o":
		if len(visible) > 0 {
			selected := visible[m.cursor]
			command, err := openCommand(m.opener, selected.Path)
			if err != nil {
				m.setError(err)
				return m, nil
			}
			return m, tea.ExecProcess(command, func(err error) tea.Msg {
				return editorFinishedMsg{name: selected.Name, err: err}
			})
		}
	case "r":
		m.reload()
		m.status, m.statusErr = "Reloaded contact notes", false
		m.loadTelegramSnapshot()
		return m.checkTelegram(m.contacts, false)
	case "esc":
		if m.telegramBusy {
			m.cancelTelegram()
			return m, nil
		}
		if m.preview {
			m.preview = false
		} else if m.query != "" {
			m.query, m.cursor = "", 0
		}
	}
	return m, nil
}

func (m Model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if (m.input == inputTelegramWaiting || m.input == inputTelegramConfirm) && msg.String() != "esc" && msg.String() != "enter" {
		return m, nil
	}
	switch msg.String() {
	case "esc":
		if m.input == inputTelegramWaiting && m.telegramCancel != nil {
			m.cancelTelegram()
		}
		m.input, m.inputValue = inputNone, ""
		return m, nil
	case "enter":
		if m.input == inputTelegramWaiting {
			return m, nil
		}
		if m.input == inputTelegram {
			return m.resolveTelegram()
		}
		if m.input == inputTelegramConfirm {
			return m.saveTelegram()
		}
		if m.input == inputSearch {
			m.input = inputNone
			return m, nil
		}
		if m.input == inputSchedule {
			return m.applySchedule()
		}
		if m.input == inputContactDate {
			return m.applyContactDate()
		}
		return m.applySnooze()
	case "backspace", "ctrl+h":
		if m.input == inputSearch {
			m.query = trimLastRune(m.query)
			m.cursor = 0
		} else {
			m.inputValue = trimLastRune(m.inputValue)
		}
		return m, nil
	case "ctrl+u":
		if m.input == inputSearch {
			m.query, m.cursor = "", 0
		} else {
			m.inputValue = ""
		}
		return m, nil
	}

	runes := msg.Runes
	if len(runes) == 0 {
		candidate := []rune(msg.String())
		if len(candidate) == 1 {
			runes = candidate
		}
	}
	for _, r := range runes {
		if m.input == inputSearch && !unicode.IsControl(r) {
			m.query += string(r)
			m.cursor = 0
		} else if m.input == inputTelegram && !unicode.IsControl(r) {
			m.inputValue += string(r)
		} else if (m.input == inputSnooze || m.input == inputSchedule) && unicode.IsDigit(r) {
			m.inputValue += string(r)
		} else if m.input == inputContactDate && (unicode.IsDigit(r) || r == '-') {
			m.inputValue += string(r)
		}
	}
	return m, nil
}

func (m Model) applySnooze() (tea.Model, tea.Cmd) {
	days, err := strconv.Atoi(m.inputValue)
	if err != nil || days <= 0 || days > 3650 {
		m.status, m.statusErr = "Enter snooze days from 1 to 3650", true
		return m, nil
	}
	visible := m.visible()
	if len(visible) == 0 {
		m.input, m.inputValue = inputNone, ""
		return m, nil
	}
	selected := visible[m.cursor]
	if err := contacts.Snooze(selected.Path, m.today, days, m.schema); err != nil {
		m.setError(err)
	} else {
		m.status, m.statusErr = fmt.Sprintf("Snoozed %s for %d days", selected.Name, days), false
		m.input, m.inputValue = inputNone, ""
		m.reload()
	}
	return m, nil
}

func (m Model) applySchedule() (tea.Model, tea.Cmd) {
	days, err := strconv.Atoi(m.inputValue)
	if err != nil || days <= 0 || days > 3650 {
		m.status, m.statusErr = "Enter an interval from 1 to 3650 days", true
		return m, nil
	}
	visible := m.visible()
	if len(visible) == 0 {
		m.input, m.inputValue = inputNone, ""
		return m, nil
	}
	selected := visible[m.cursor]
	if err := contacts.Schedule(selected.Path, days, m.schema); err != nil {
		m.setError(err)
	} else {
		m.status, m.statusErr = fmt.Sprintf("Scheduled %s every %d days", selected.Name, days), false
		m.input, m.inputValue = inputNone, ""
		m.reload()
	}
	return m, nil
}

func (m Model) applyContactDate() (tea.Model, tea.Cmd) {
	value := m.inputValue
	if value == "" {
		value = m.today.Format("2006-01-02")
	}
	contacted, err := time.ParseInLocation("2006-01-02", value, m.today.Location())
	if err != nil {
		m.status, m.statusErr = "Enter the contact date as YYYY-MM-DD", true
		return m, nil
	}
	if contacted.After(m.today) {
		m.status, m.statusErr = "Last contacted date cannot be in the future", true
		return m, nil
	}
	visible := m.visible()
	if len(visible) == 0 {
		m.input, m.inputValue = inputNone, ""
		return m, nil
	}
	selected := visible[m.cursor]
	if err := contacts.MarkContacted(selected.Path, contacted, m.schema); err != nil {
		m.setError(err)
	} else {
		m.status, m.statusErr = "Marked "+selected.Name+" as contacted on "+contacted.Format("2 January 2006"), false
		m.input, m.inputValue = inputNone, ""
		m.reload()
	}
	return m, nil
}

func (m *Model) reload() {
	loaded, warnings := contacts.Scan(m.dir, m.schema)
	sort.SliceStable(loaded, func(i, j int) bool {
		if loaded[i].Tracked != loaded[j].Tracked {
			return loaded[i].Tracked
		}
		if !loaded[i].Tracked {
			return strings.ToLower(loaded[i].Name) < strings.ToLower(loaded[j].Name)
		}
		left, right := loaded[i].DueDate(m.today), loaded[j].DueDate(m.today)
		if left.Equal(right) {
			return strings.ToLower(loaded[i].Name) < strings.ToLower(loaded[j].Name)
		}
		return left.Before(right)
	})
	m.contacts, m.warnings = loaded, warnings
	if m.cursor >= len(m.visible()) {
		m.cursor = max(0, len(m.visible())-1)
	}
}

func (m Model) visible() []contacts.Contact {
	query := strings.ToLower(strings.TrimSpace(m.query))
	visible := make([]contacts.Contact, 0, len(m.contacts))
	for _, contact := range m.contacts {
		days := contact.DaysUntil(m.today)
		inView := m.view == viewAll || (contact.Tracked && m.view == viewKeepInTouch) || (contact.Tracked && m.view == viewDue && days <= 0) || (contact.Tracked && m.view == viewUpcoming && days > 0)
		matches := query == "" || strings.Contains(strings.ToLower(contact.Name), query) || strings.Contains(strings.ToLower(contact.Note), query)
		if inView && matches {
			visible = append(visible, contact)
		}
	}
	return visible
}

func (m Model) View() string {
	var out strings.Builder
	out.WriteString(titleStyle.Render("wassup"))
	headerDetails := "  " + m.today.Format("2 January 2006") + "  " + m.dir
	out.WriteString(dimStyle.Render(truncate(headerDetails, max(10, m.width-8))))
	out.WriteString("\n\n")
	out.WriteString(m.tabs())
	out.WriteString("\n\n")

	visible := m.visible()
	footer := m.footer()
	footerHeight := 0
	for line := range strings.Lines(footer) {
		footerHeight += max(1, (lipgloss.Width(strings.TrimSuffix(line, "\n"))+max(1, m.width)-1)/max(1, m.width))
	}
	contentHeight := max(1, m.height-6-footerHeight)
	narrowPreview := ""
	if m.preview && len(visible) > 0 && m.width < 100 {
		narrowPreview = m.previewView(visible[m.cursor], min(m.width, 100), min(10, max(3, m.height/3)))
		contentHeight = max(1, contentHeight-lipgloss.Height(narrowPreview)-2)
	}
	if m.preview && len(visible) > 0 && m.width >= 100 {
		leftWidth := max(48, m.width*52/100)
		rightWidth := max(40, m.width-leftWidth-2)
		listPane := lipgloss.NewStyle().Width(leftWidth).Render(m.listView(visible, leftWidth, contentHeight))
		previewPane := m.previewView(visible[m.cursor], rightWidth, contentHeight)
		out.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, listPane, "  ", previewPane))
	} else {
		out.WriteString(m.listView(visible, m.width, contentHeight))
		if narrowPreview != "" {
			out.WriteString("\n\n")
			out.WriteString(narrowPreview)
		}
	}
	out.WriteByte('\n')
	out.WriteString(footer)
	return out.String()
}

func (m Model) listView(visible []contacts.Contact, width, limit int) string {
	if len(visible) == 0 {
		return dimStyle.Render("No contacts in this view.")
	}
	var out strings.Builder
	start := 0
	showMore := len(visible) > limit && limit > 1
	if showMore {
		limit--
	}
	if m.cursor >= limit {
		start = m.cursor - limit + 1
	}
	end := min(len(visible), start+limit)
	for index := start; index < end; index++ {
		out.WriteString(m.row(visible[index], index == m.cursor, width))
		if index+1 < end || end < len(visible) {
			out.WriteByte('\n')
		}
	}
	if end < len(visible) && showMore {
		out.WriteString(dimStyle.Render(fmt.Sprintf("  … %d more", len(visible)-end)))
	}
	return out.String()
}

func (m Model) tabs() string {
	labels := []string{"Due", "Upcoming", "Keep in touch", "All"}
	parts := make([]string, len(labels))
	for index, label := range labels {
		count := m.count(viewMode(index))
		text := fmt.Sprintf("%s %d", label, count)
		if int(m.view) == index {
			parts[index] = activeStyle.Render(text)
		} else {
			parts[index] = inactiveStyle.Render(text)
		}
	}
	return strings.Join(parts, " ")
}

func (m Model) count(view viewMode) int {
	count := 0
	for _, contact := range m.contacts {
		days := contact.DaysUntil(m.today)
		if view == viewAll || (contact.Tracked && view == viewKeepInTouch) || (contact.Tracked && view == viewDue && days <= 0) || (contact.Tracked && view == viewUpcoming && days > 0) {
			count++
		}
	}
	return count
}

func (m Model) row(contact contacts.Contact, selected bool, width int) string {
	prefix := "  "
	if selected {
		prefix = cursorStyle.Render("› ")
	}
	nameWidth := min(36, max(12, width-34))
	name := truncate(contact.Name, nameWidth)
	name = lipgloss.NewStyle().Width(nameWidth).Render(name)
	due := "           "
	status := dimStyle.Render("not scheduled")
	if contact.Tracked {
		due = contact.DueDate(m.today).Format("02 Jan 2006")
		status = dueLabel(contact.DaysUntil(m.today))
	}
	return fmt.Sprintf("%s%s%s  %s  %s", prefix, m.telegramBadge(contact.Path), name, due, status)
}

func dueLabel(days int) string {
	switch {
	case days < 0:
		return overdueStyle.Render(fmt.Sprintf("%d days overdue", -days))
	case days == 0:
		return todayStyle.Render("due today")
	case days == 1:
		return soonStyle.Render("tomorrow")
	default:
		return soonStyle.Render(fmt.Sprintf("in %d days", days))
	}
}

func (m Model) previewView(contact contacts.Contact, width, maxLines int) string {
	contentText := contact.Note
	if details := m.telegramDetails(contact); details != "" {
		contentText = details + "\n\n" + contentText
	}
	lines := strings.Split(contentText, "\n")
	if len(lines) > maxLines {
		lines = append(lines[:maxLines], "…")
	}
	content := strings.Join(lines, "\n")
	innerWidth := max(1, width-4) // border and horizontal padding consume four cells
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("238")).Padding(0, 1).Width(innerWidth).MaxHeight(maxLines).Render(content)
}

func (m Model) footer() string {
	var prompt string
	switch m.input {
	case inputTelegram, inputTelegramConfirm, inputTelegramWaiting:
		prompt = m.telegramPrompt()
	case inputSearch:
		prompt = "Search: " + m.query + "█  " + dimStyle.Render("enter accept · esc cancel")
	case inputSnooze:
		prompt = "Snooze days: " + m.inputValue + "█  " + dimStyle.Render("enter apply · esc cancel")
	case inputSchedule:
		prompt = "Contact every days: " + m.inputValue + "█  " + dimStyle.Render("enter apply · esc cancel")
	case inputContactDate:
		prompt = "Last contacted (YYYY-MM-DD): " + m.inputValue + "█  " + dimStyle.Render("blank means today · enter apply · esc cancel")
	default:
		prompt = dimStyle.Render("j/k move · tab or [ ] views · / search · enter preview · o open · t Telegram · v verify · V verify all · a schedule · x remove · d contacted · s snooze · r reload · q quit")
		if len(m.telegramLinks) > 0 {
			prompt += "\n" + dimStyle.Render("Telegram: ✈✓ verified · ✈~ details changed · ✈? unchecked · ✈! unavailable")
		}
	}
	if m.telegramBusy {
		prompt += "\n" + todayStyle.Render(m.telegramActivity())
	}
	if m.query != "" && m.input != inputSearch {
		prompt += "\n" + dimStyle.Render("Filter: ") + m.query
	}
	if m.status != "" {
		style := successStyle
		if m.statusErr {
			style = errorStyle
		}
		prompt += "\n" + style.Render(m.status)
	}
	if len(m.warnings) > 0 {
		prompt += "\n" + errorStyle.Render(fmt.Sprintf("%d note(s) skipped because of invalid scheduling fields", len(m.warnings)))
	}
	return lipgloss.NewStyle().Width(max(1, m.width)).Render(prompt)
}

func (m *Model) setError(err error) {
	m.status, m.statusErr = err.Error(), true
}

func trimLastRune(value string) string {
	runes := []rune(value)
	if len(runes) == 0 {
		return ""
	}
	return string(runes[:len(runes)-1])
}

func truncate(value string, width int) string {
	if lipgloss.Width(value) <= width {
		return value
	}
	if width <= 1 {
		if width == 1 {
			return "…"
		}
		return ""
	}
	var result strings.Builder
	for _, r := range value {
		candidate := result.String() + string(r)
		if lipgloss.Width(candidate) > width-1 {
			break
		}
		result.WriteRune(r)
	}
	return result.String() + "…"
}

func startOfDay(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, t.Location())
}

func openCommand(template []string, path string) (*exec.Cmd, error) {
	if len(template) == 0 || strings.TrimSpace(template[0]) == "" {
		return nil, fmt.Errorf("opener command is empty")
	}
	arguments := make([]string, len(template))
	hasPath := false
	for index, argument := range template {
		hasPath = hasPath || strings.Contains(argument, "{path}")
		arguments[index] = strings.ReplaceAll(argument, "{path}", path)
	}
	if !hasPath {
		return nil, fmt.Errorf("opener command has no {path} placeholder")
	}
	return exec.Command(arguments[0], arguments[1:]...), nil
}
