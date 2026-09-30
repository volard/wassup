# wassup

![CI](https://github.com/volard/wassup/actions/workflows/release.yml/badge.svg)
![Go 1.24+](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white)
![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)

A local TUI that reminds you who you have not spoken to lately—without moving
your contact notes out of Markdown.

![Wassup terminal demo](docs/demo.gif)

Markdown remains the source of truth. Wassup validates contact metadata and
updates notes when you change reminders or import Telegram details.

## Quick start

Install the latest Linux or macOS release (`amd64` or `arm64`):

```sh
curl -fsSL https://raw.githubusercontent.com/volard/wassup/main/install.sh | sh
```

Binary is saved by default at `~/.local/bin/wassup`. Set `INSTALL_PATH` to install somewhere else.

Point Wassup at your contact notes on first run:

```sh
wassup --contacts /path/to/contacts
```

The path is saved in `~/.config/wassup/config.json`; later runs need only
`wassup`.

### Build from source

```sh
make build
./build/wassup --contacts /path/to/contacts
```

## Contact notes

Each contact is a Markdown file with frontmatter. For example:

```yaml
---
cnt:name: Alex Morgan
cnt:last-contact: 2026-08-28
cnt:contact-every-days: 90
cnt:snooze-until: 2026-09-15
---
```

`cnt:contact-every-days` schedules a contact. Without it, the note remains in
**All** but not **Keep in touch**. A scheduled contact without
`cnt:last-contact` is due today. Dates use the local timezone; birth dates also
accept `--MM-DD` when the year is unknown.

Use `wassup --check` to validate every note without opening the TUI.

Fields could be changed in configuration.

## Configuration

Wassup creates `~/.config/wassup/config.json` on first run

```jsonc
{
  "contacts_dir": "/path/to/contacts",
  "start_view": "keep_in_touch", // due, upcoming, keep_in_touch, or all
  "opener": { "command": ["nvim", "{path}"] }, // Used by `o`
  "notifications": {
    "daily_at": "18:00",
    "max_suggestions": 1,
    "command": ["notify-send", "--app-name=wassup", "--urgency=normal", "{title}", "{body}"]
  },
  "telegram": { "check_every_days": 14 }, // 0 disables automatic checks
  "frontmatter_fields": {
    "display_name": "cnt:name",
    "fallback_name": "name",
    "birth_date": "cnt:birth-date",
    "relationship_origin": "rel:origin",
    "relationship_context": "rel:context",
    "photo": "cnt:photo",
    "phone": "cnt:phone",
    "email": "cnt:email",
    "telegram": "cnt:telegram",
    "last_contact": "cnt:last-contact",
    "contact_every_days": "cnt:contact-every-days",
    "snooze_until": "cnt:snooze-until",
    "relationship_origins": ["family", "friend", "school", "university", "work"]
  }
}
```

Commands are executed directly, not through a shell. Relative `contacts_dir`
paths resolve from the config directory. `--config` selects another config;
`--contacts` and `WASSUP_CONTACTS` temporarily override its contact path.
Precedence is CLI → environment → config.

## Telegram sync

Telegram sync advances last-contact dates from your outgoing private messages.
It can also fill empty birthday and phone fields. Incoming messages do not count.

Create an app at [my.telegram.org](https://my.telegram.org) under **API
development tools**, then connect your account:

```sh
wassup telegram login
```

In the TUI, select a contact and press `t` to find and confirm their Telegram
account. Alternatively, link contacts from the terminal:

```sh
wassup telegram link
```

Preview changes before applying them:

```sh
wassup telegram sync --dry-run
wassup telegram sync
```

The TUI checks linked accounts every 14 days when opened or reloaded. Press `v`
to check one contact or `V` to check all now. Set
`telegram.check_every_days` to `0` for manual checks only. Sync itself runs only
when requested.

## Notifications

Requirements:

- Linux
- systemd user session
- `notify-send`

Can be enabled with:

```sh
wassup --install-notifications
```

