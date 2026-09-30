package telegramsync

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"golang.org/x/sys/unix"
	"golang.org/x/term"

	"wassup/internal/contacts"
)

const Usage = `Telegram contact sync:
  wassup [--config PATH] [--contacts DIR] telegram login
  wassup [--config PATH] [--contacts DIR] telegram link [--note FILE] [--user USERNAME_OR_ID]
  wassup [--config PATH] [--contacts DIR] telegram sync [--dry-run]

Login prompts locally for api_id, api_hash, phone, login code, and 2FA if needed.
Link confirms each selection; --note with --user explicitly saves that mapping.
Sync reads outgoing personal messages and only advances last-contact dates.
Global --config and --contacts flags must precede 'telegram'.
`

type commandOptions struct {
	action, note, user string
	dryRun             bool
}

func parseCommand(args []string, out io.Writer) (commandOptions, error) {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(out, Usage)
		return commandOptions{}, flag.ErrHelp
	}
	opts := commandOptions{action: args[0]}
	flags := flag.NewFlagSet("telegram "+opts.action, flag.ContinueOnError)
	flags.SetOutput(out)
	flags.Usage = func() { fmt.Fprint(out, Usage); flags.PrintDefaults() }
	switch opts.action {
	case "login":
	case "link":
		flags.StringVar(&opts.note, "note", "", "note filename or absolute path within the configured contacts directory")
		flags.StringVar(&opts.user, "user", "", "Telegram username, profile URL, or user ID from your contacts/chats")
	case "sync":
		flags.BoolVar(&opts.dryRun, "dry-run", false, "preview note changes without writing them")
	default:
		return opts, fmt.Errorf("unknown Telegram command %q; use 'wassup telegram help'", opts.action)
	}
	if err := flags.Parse(args[1:]); err != nil {
		return opts, err
	}
	if flags.NArg() != 0 {
		return opts, fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	if opts.user != "" && opts.note == "" {
		return opts, errors.New("--user requires --note so the intended contact is explicit")
	}
	return opts, nil
}

// Run never requests login interactively during a sync, making dry runs and
// scheduled invocations predictable. Only 'login' obtains new credentials.
func Run(ctx context.Context, args []string, configPath, contactsDir string, schema contacts.Schema, in *os.File, out, errOut io.Writer) error {
	opts, err := parseCommand(args, out)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	var found []contacts.Contact
	if opts.action != "login" {
		var warnings []error
		found, warnings = contacts.Scan(contactsDir, schema)
		for _, warning := range warnings {
			fmt.Fprintln(errOut, warning)
		}
		if len(found) == 0 {
			return errors.New("no valid contact notes found")
		}
		if opts.note != "" {
			path := opts.note
			if !filepath.IsAbs(path) {
				path = filepath.Join(contactsDir, path)
			}
			path = filepath.Clean(path)
			var selected []contacts.Contact
			for _, c := range found {
				if c.Path == path {
					selected = append(selected, c)
				}
			}
			if len(selected) != 1 {
				return errors.New("--note must identify a valid note in the configured contacts directory")
			}
			found = selected
		}
	}
	dir := StateDir(configPath)
	unlock, err := lockState(dir)
	if err != nil {
		return err
	}
	defer unlock()
	prompt := &terminalPrompt{in: in, out: out, ctx: ctx}
	prompt.reader = bufio.NewReader(promptReader{prompt})
	credsPath := filepath.Join(dir, "credentials.json")
	var creds credentials
	if err := readJSON(credsPath, &creds); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if opts.action != "login" {
			return errors.New("Telegram is not configured; run 'wassup telegram login' first")
		}
		id, err := prompt.ask("Telegram api_id (from my.telegram.org): ", false)
		if err != nil {
			return err
		}
		creds.AppID, err = strconv.Atoi(id)
		if err != nil {
			return errors.New("api_id must be a positive integer")
		}
		creds.AppHash, err = prompt.ask("Telegram api_hash: ", true)
		if err != nil {
			return err
		}
		if err := creds.validate(); err != nil {
			return err
		}
		if err := writeJSON(credsPath, creds); err != nil {
			return err
		}
	}
	if err := creds.validate(); err != nil {
		return err
	}
	sessionPath := filepath.Join(dir, "session.json")
	if opts.action != "login" {
		if _, err := privateRead(sessionPath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return errors.New("no Telegram session; run 'wassup telegram login' first")
			}
			return err
		}
	}
	client := telegram.NewClient(creds.AppID, creds.AppHash, telegram.Options{
		SessionStorage: sessionStorage{path: sessionPath}, NoUpdates: true,
		Device: telegram.DeviceConfig{DeviceModel: "Wassup", SystemVersion: "Desktop", AppVersion: "1.0", SystemLangCode: "en", LangCode: "en"},
	})
	err = client.Run(ctx, func(ctx context.Context) error {
		prompt.ctx = ctx
		if opts.action == "login" {
			if err := client.Auth().IfNecessary(ctx, auth.NewFlow(prompt, auth.SendCodeOptions{})); err != nil {
				return fmt.Errorf("Telegram login: %w", err)
			}
		}
		status, err := client.Auth().Status(ctx)
		if err != nil {
			return err
		}
		if !status.Authorized || status.User == nil {
			return errors.New("Telegram session expired; run 'wassup telegram login'")
		}
		if status.User.Bot {
			return errors.New("sign in with a personal account")
		}
		linksPath := filepath.Join(dir, "links.json")
		saved, err := loadLinks(linksPath, status.User.ID)
		if err != nil {
			return err
		}
		if opts.action == "login" {
			fmt.Fprintf(out, "Connected to Telegram account %d. Session saved in %s\nNext: wassup telegram link\n", status.User.ID, dir)
			return nil
		}
		remote := &remoteClient{api: client.API()}
		if opts.action == "link" {
			return linkNotes(ctx, remote, found, &saved, linksPath, opts, prompt, out)
		}
		fmt.Fprintln(out, "Checking linked personal chats…")
		return syncNotes(ctx, remote, found, saved, schema, opts.dryRun, out)
	})
	if err != nil {
		return err
	}
	return ctx.Err()
}

type peopleSource interface {
	People(context.Context) ([]Person, error)
	Resolve(context.Context, string) (Person, error)
}

func linkNotes(ctx context.Context, remote peopleSource, found []contacts.Contact, saved *links, path string, opts commandOptions, prompt *terminalPrompt, out io.Writer) error {
	fmt.Fprintln(out, "Loading Telegram contacts and personal chats…")
	people, err := remote.People(ctx)
	if err != nil {
		return err
	}
	lookup := func(value string) (Person, error) {
		if id, err := strconv.ParseInt(value, 10, 64); err == nil {
			for _, p := range people {
				if p.ID == id {
					return p, nil
				}
			}
			return Person{}, errors.New("user ID is not in your contacts or personal chats")
		}
		return remote.Resolve(ctx, value)
	}
	for _, c := range found {
		if err := ctx.Err(); err != nil {
			return err
		}
		if p, ok := saved.Notes[c.Path]; ok && opts.note == "" {
			fmt.Fprintf(out, "Already linked: %s → %s (%d)\n", c.Name, p.Name, p.ID)
			continue
		}
		choice := opts.user
		if choice == "" {
			fmt.Fprintf(out, "\n%s [%s]\n", c.Name, filepath.Base(c.Path))
			candidates := suggestions(c, people)
			for _, p := range candidates {
				printPerson(out, p)
			}
			if c.Telegram != "" {
				fmt.Fprintf(out, "Note Telegram field: %s\n", c.Telegram)
			}
			for {
				choice, err = prompt.ask("Username, user ID, /name to search, or Enter to skip: ", false)
				if err != nil {
					return err
				}
				if !strings.HasPrefix(choice, "/") {
					break
				}
				query := strings.TrimSpace(strings.TrimPrefix(choice, "/"))
				count := 0
				for _, p := range people {
					if query == "" || strings.Contains(strings.ToLower(p.Name+" "+p.Username), strings.ToLower(query)) {
						printPerson(out, p)
						count++
						if count == 30 {
							fmt.Fprintln(out, "Showing up to 30 matches; refine the search if needed.")
							break
						}
					}
				}
				if count == 0 {
					fmt.Fprintln(out, "No matching people.")
				}
			}
			if choice == "" {
				continue
			}
		}
		person, err := lookup(choice)
		if err != nil {
			return fmt.Errorf("link %s: %w", c.Name, err)
		}
		if opts.user == "" {
			printPerson(out, person)
			answer, err := prompt.ask("Link this person to "+c.Name+"? [y/N]: ", false)
			if err != nil {
				return err
			}
			if !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
				continue
			}
		}
		if err := saved.bind(c.Path, person); err != nil {
			return err
		}
		if err := writeJSON(path, saved); err != nil {
			return err
		}
		fmt.Fprintf(out, "Linked %s → %s (%d)\n", c.Name, person.Name, person.ID)
	}
	fmt.Fprintln(out, "Next: wassup telegram sync --dry-run")
	return nil
}

func printPerson(out io.Writer, p Person) {
	fmt.Fprintf(out, "  %d  %s  @%s\n", p.ID, p.Name, p.Username)
}

type terminalPrompt struct {
	in     *os.File
	reader *bufio.Reader
	out    io.Writer
	ctx    context.Context
}

func (p *terminalPrompt) ask(label string, secret bool) (string, error) {
	if secret {
		if p.in == nil || !term.IsTerminal(int(p.in.Fd())) {
			return "", errors.New("enter credentials in an interactive terminal; secret input cannot be piped")
		}
		state, err := term.MakeRaw(int(p.in.Fd()))
		if err != nil {
			return "", err
		}
		defer term.Restore(int(p.in.Fd()), state)
		terminal := term.NewTerminal(struct {
			io.Reader
			io.Writer
		}{promptReader{p}, p.out}, "")
		value, err := terminal.ReadPassword(label)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(value), nil
	}
	fmt.Fprint(p.out, label)
	value, err := p.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

// Poll between reads so Ctrl-C and connection cancellation also interrupt
// prompts. Secret prompts restore the terminal before returning on cancellation.
type promptReader struct{ prompt *terminalPrompt }

func (r promptReader) Read(data []byte) (int, error) {
	p := r.prompt
	for {
		if p.ctx != nil {
			if err := p.ctx.Err(); err != nil {
				return 0, err
			}
		}
		fds := []unix.PollFd{{Fd: int32(p.in.Fd()), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, 100)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return 0, err
		}
		if n > 0 {
			return p.in.Read(data)
		}
	}
}
func (p *terminalPrompt) Phone(context.Context) (string, error) {
	return p.ask("Telegram phone number (international format): ", false)
}
func (p *terminalPrompt) Password(context.Context) (string, error) {
	return p.ask("Telegram two-step verification password: ", true)
}
func (p *terminalPrompt) Code(_ context.Context, _ *tg.AuthSentCode) (string, error) {
	return p.ask("Telegram login code: ", true)
}
func (p *terminalPrompt) SignUp(context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("create your Telegram account in the official app first")
}
func (p *terminalPrompt) AcceptTermsOfService(context.Context, tg.HelpTermsOfService) error {
	return errors.New("complete Telegram account setup in the official app first")
}
