package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"wassup/internal/config"
	"wassup/internal/contacts"
	"wassup/internal/notifications"
	telegramsync "wassup/internal/telegram"
	"wassup/internal/ui"
)

func main() {
	defaultConfigPath, err := config.DefaultPath()
	if err != nil {
		fatal(fmt.Errorf("find user config directory: %w", err))
	}
	configPath := flag.String("config", defaultConfigPath, "configuration file path")
	contactsOverride := flag.String("contacts", "", "override the configured contacts directory")
	check := flag.Bool("check", false, "validate contact frontmatter and exit")
	notify := flag.Bool("notify", false, "send today's due-contact desktop notification and exit")
	installNotifications := flag.Bool("install-notifications", false, "install and enable the daily desktop notification timer")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "Usage: wassup [options] [telegram login|link|sync|help]")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() > 0 {
		if flag.Arg(0) != "telegram" {
			fatal(fmt.Errorf("unknown command %q", flag.Arg(0)))
		}
		if enabledFlags(*check, *notify, *installNotifications) > 0 {
			fatal(errors.New("telegram cannot be combined with --check, --notify, or --install-notifications"))
		}
	}
	if enabledFlags(*check, *notify, *installNotifications) > 1 {
		fatal(errors.New("use only one of --check, --notify, or --install-notifications"))
	}

	absConfigPath, err := filepath.Abs(*configPath)
	if err != nil {
		fatal(fmt.Errorf("config path: %w", err))
	}
	bootstrapDir := firstNonempty(*contactsOverride, os.Getenv("WASSUP_CONTACTS"), defaultContactsDir())
	absBootstrapDir, err := filepath.Abs(bootstrapDir)
	if err != nil {
		fatal(fmt.Errorf("contacts directory: %w", err))
	}
	settings, created, err := config.LoadOrCreate(absConfigPath, config.Default(absBootstrapDir))
	if err != nil {
		fatal(err)
	}

	contactsDir, err := settings.ResolveContactsDir(absConfigPath)
	if err != nil {
		fatal(fmt.Errorf("contacts directory: %w", err))
	}
	if override := firstNonempty(*contactsOverride, os.Getenv("WASSUP_CONTACTS")); override != "" {
		contactsDir, err = filepath.Abs(override)
		if err != nil {
			fatal(fmt.Errorf("contacts directory override: %w", err))
		}
	}
	if flag.NArg() > 0 {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := telegramsync.Run(ctx, flag.Args()[1:], absConfigPath, contactsDir, settings.Fields, os.Stdin, os.Stdout, os.Stderr); err != nil {
			fatal(err)
		}
		return
	}
	info, err := os.Stat(contactsDir)
	if err != nil {
		fatal(fmt.Errorf("contacts directory: %w", err))
	}
	if !info.IsDir() {
		fatal(fmt.Errorf("contacts path is not a directory: %s", contactsDir))
	}
	if *check {
		found, warnings := contacts.Scan(contactsDir, settings.Fields)
		for _, warning := range warnings {
			fmt.Fprintln(os.Stderr, warning)
		}
		if len(warnings) > 0 {
			os.Exit(1)
		}
		fmt.Printf("Validated %d contact notes\n", len(found))
		return
	}
	if *notify {
		found, warnings := contacts.Scan(contactsDir, settings.Fields)
		for _, warning := range warnings {
			fmt.Fprintln(os.Stderr, warning)
		}
		digest, exists := notifications.BuildDigest(found, time.Now(), settings.Notifications.MaxSuggestions)
		if !exists {
			fmt.Println("No contacts are due; no notification sent")
			return
		}
		sent, err := notifications.SendOnce(settings.Notifications.Command, digest, notifications.StatePath(absConfigPath), time.Now())
		if err != nil {
			fatal(err)
		}
		if sent {
			fmt.Printf("Sent desktop notification: %s\n", digest.Title)
		} else {
			fmt.Println("Today's desktop notification was already sent")
		}
		return
	}
	if *installNotifications {
		executable, err := os.Executable()
		if err != nil {
			fatal(fmt.Errorf("find wassup executable: %w", err))
		}
		executable, err = filepath.EvalSymlinks(executable)
		if err != nil {
			fatal(fmt.Errorf("resolve wassup executable: %w", err))
		}
		timerPath, err := notifications.InstallSystemd(executable, absConfigPath, settings.Notifications.DailyAt)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("Enabled daily desktop notifications at %s via %s\n", settings.Notifications.DailyAt, timerPath)
		return
	}

	status := ""
	if created {
		status = "Created configuration: " + absConfigPath
	}
	model := ui.New(contactsDir, settings.Fields, settings.StartView, settings.Opener.Command, status).
		WithTelegram(telegramsync.Service{ConfigPath: absConfigPath, Schema: settings.Fields}, settings.Telegram.CheckEveryDays)
	program := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		fatal(err)
	}
}

func enabledFlags(values ...bool) int {
	count := 0
	for _, value := range values {
		if value {
			count++
		}
	}
	return count
}

func defaultContactsDir() string {
	candidate := filepath.Join("resources", "contacts")
	if info, err := os.Stat(candidate); err == nil && info.IsDir() {
		return candidate
	}
	return "."
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "wassup:", err)
	os.Exit(1)
}
