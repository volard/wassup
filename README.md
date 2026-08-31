# wassup

`wassup` is a local terminal UI that reminds you who you have not spoken to
lately without moving your contact notes out of Markdown. The app writes only
the three optional scheduling properties below and validates the configured
contact metadata fields when loading notes.

```yaml
cnt:last-contact: 2026-08-28
cnt:contact-every-days: 90
cnt:snooze-until: 2026-09-15
```

A note becomes scheduled when `cnt:contact-every-days` is present. Unscheduled
notes remain visible under **All** and can be scheduled from the app.

## Install

Linux release binaries are available for `amd64` and `arm64`. Because the
repository is private, authenticate the GitHub CLI before running the installer:

```sh
gh auth login
gh api repos/volard/wassup/contents/install.sh \
  -H "Accept: application/vnd.github.raw+json" | sh
```

The script verifies the release checksum and installs to `~/.local/bin/wassup`.
Set `INSTALL_DIR` to choose another directory or `VERSION=v1.2.3` to install a
specific release. The token returned by `gh auth token` must have access to this
repository.

## Run

On the first run, provide the contacts directory once:

```sh
wassup --contacts /path/to/notes/resources/contacts
```

`wassup` creates a configuration file containing that absolute path. Later runs
need no arguments.

To build from source with Go 1.24 or newer:

```sh
make build
./build/wassup --contacts /path/to/notes/resources/contacts
```

Build artifacts stay in the ignored `build/` directory. Pushing a `v*` tag runs
tests and publishes static Linux `amd64` and `arm64` binaries with SHA-256
checksums to the corresponding GitHub Release.

## Configuration

The default location follows the operating system's user configuration
directory. On Linux it is usually:

```text
~/.config/wassup/config.json
```

The generated file contains every supported setting:

```json
{
  "contacts_dir": "/path/to/notes/resources/contacts",
  "start_view": "keep_in_touch",
  "opener": {
    "command": ["nvim", "{path}"]
  },
  "notifications": {
    "daily_at": "18:00",
    "max_suggestions": 1,
    "command": [
      "notify-send",
      "--app-name=wassup",
      "--urgency=normal",
      "{title}",
      "{body}"
    ]
  },
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
    "relationship_origins": [
      "family",
      "friend",
      "school",
      "university",
      "work"
    ]
  }
}
```

`start_view` controls the view shown at launch. Accepted values are:

- `due`
- `upcoming`
- `keep_in_touch`
- `all`

`opener.command` configures the program launched by `o`. It is an argument
array, not a shell command, and must contain `{path}`. This safely supports paths
containing spaces. For example:

```json
"opener": {
  "command": ["nvim", "{path}"]
}
```

Edit the field values if your notes use different frontmatter keys, then restart
the app. Field keys must be nonempty and unique. `relationship_origins` is the
closed vocabulary accepted by `rel:origin`; identifiers must be lowercase and
contain no spaces. Birth dates accept `YYYY-MM-DD`, or `--MM-DD` when the year is
unknown. The app reports malformed JSON, unknown settings, duplicate field keys,
unknown or duplicate `cnt:`/`rel:` fields, invalid contact metadata, and missing
contact directories instead of replacing the configuration. Other frontmatter
namespaces remain available for unrelated note metadata.

Validate every contact without opening the TUI:

```sh
wassup --check
```

A relative `contacts_dir` is resolved relative to the configuration file—not
the terminal's current directory.

Use another config file with:

```sh
wassup --config /path/to/config.json
```

The `--contacts` flag and `WASSUP_CONTACTS` environment variable temporarily
override `contacts_dir`; they do not overwrite an existing config. CLI takes
precedence over the environment, which takes precedence over the config.

You can also bootstrap the default directory through the environment:

```sh
export WASSUP_CONTACTS=/path/to/notes/resources/contacts
wassup
```

When creating a config and no path override is supplied, `wassup` uses
`./resources/contacts` if it exists, otherwise the current directory.

## Desktop notifications

Install and enable the local daily reminder:

```sh
wassup --install-notifications
```

This creates a systemd user timer; it does not run a server or keep Wassup open.
At `notifications.daily_at`, Wassup sends one desktop digest through the
configured command. `max_suggestions` limits how many names it includes even
when many contacts are due. `Persistent=true` makes a reminder missed while the
PC was off run after the next login. A successful notification is recorded in
`~/.config/wassup/notification-state`, preventing repeated reminders that day.

Run the notification check manually with:

```sh
wassup --notify
```

The default command uses `notify-send`. Arguments are executed directly without
a shell and must contain `{title}` and `{body}` placeholders.

## Keys

| Key | Action |
| --- | --- |
| `j`, `k`, arrows | Move |
| `tab`, `]` | Move to the next Due / Upcoming / Keep in touch / All view |
| `shift+tab`, `[` | Move to the previous view |
| `/` | Search names and note bodies |
| `enter` | Toggle note preview |
| `o` | Open the selected contact with the configured program |
| `a` | Schedule or change the selected contact interval |
| `x` | Remove scheduling and the person from Keep in touch |
| `d` | Set the selected contact's last-contacted date (`YYYY-MM-DD`; blank means today) |
| `s` | Snooze the selected contact |
| `r` | Reload files from disk |
| `esc` | Close preview or clear search |
| `q` | Quit |

The note preview is open by default. It appears beside the contact list on
terminals at least 100 columns wide; on narrower terminals it is stacked below
the list. It shows the configured identity and relationship frontmatter before
the Markdown body; internal scheduling fields are omitted. Press `enter` to hide
or show it.

**All** contains every Markdown contact. **Keep in touch** contains only contacts
with the configured `contact_every_days` field. Removing someone from Keep in
touch preserves their note and `last_contact`; it removes only their interval
and snooze fields.

Dates use the machine's local timezone. A scheduled contact without a
`cnt:last-contact` date is due today.

Pressing `d` opens a date prompt. Enter the date when you actually last spoke,
or leave it blank and press Enter to use today. Future dates are rejected.

## File safety

Updates are written to a temporary file in the same directory and atomically
renamed into place. The app replaces, adds, or removes only its own scheduling
lines; unrelated frontmatter and the Markdown body are preserved.

Malformed scheduling values cause that note to be skipped and produce a warning
in the footer. Valid notes continue to load.
