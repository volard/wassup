# wassup

[![CI](https://github.com/volard/wassup/actions/workflows/release.yml/badge.svg)](https://github.com/volard/wassup/actions/workflows/release.yml)
[![Go 1.24+](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](LICENSE)

A local TUI that reminds you who you have not spoken to lately—without moving
your contact notes out of Markdown.

![Wassup terminal demo](docs/demo.gif)

Markdown remains the source of truth. Wassup validates contact metadata and
only changes its three scheduling fields.

## Quick start

Install the latest Linux or macOS release (`amd64` or `arm64`):

```sh
curl -fsSL https://raw.githubusercontent.com/volard/wassup/main/install.sh | sh
```

The installer verifies the published checksum and writes to
`~/.local/bin/wassup`. Set `INSTALL_PATH` to install somewhere else.

Point Wassup at your contact notes on first run:

```sh
wassup --contacts /path/to/contacts
```

The path is saved in `~/.config/wassup/config.json`; later runs need only
`wassup`.

### Build from source

Requires Go 1.24 or newer:

```sh
make build
./build/wassup --contacts /path/to/contacts
```

Build output stays in the ignored `build/` directory.

## Contact notes

Each contact is a Markdown file with frontmatter:

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

## Configuration

The generated config contains every supported setting and frontmatter mapping:

- `contacts_dir`: directory containing contact notes
- `start_view`: `due`, `upcoming`, `keep_in_touch`, or `all`
- `opener.command`: argument array used by `o`; must contain `{path}`
- `frontmatter_fields`: contact keys and allowed relationship origins
- `notifications`: reminder time, suggestion limit, and notification command
- `telegram.check_every_days`: automatic account-check interval; defaults to 14, or 0 for manual only

Commands are executed directly, not through a shell. Relative `contacts_dir`
paths resolve from the config directory. `--config` selects another config;
`--contacts` and `WASSUP_CONTACTS` temporarily override its contact path.
Precedence is CLI → environment → config.

## Telegram sync

Connect your personal Telegram account to advance last-contact dates from your
latest outgoing messages in private chats. Uses the existing contacts directory
and frontmatter mappings. Incoming messages alone do not reset reminders.

Create an application at [my.telegram.org](https://my.telegram.org) → **API
development tools** to obtain an `api_id` and `api_hash`. Then run:

```sh
wassup telegram login
wassup
wassup telegram sync --dry-run
wassup telegram sync
```

When running from source, use `make build` and replace `wassup` above with
`./build/wassup`. Login prompts locally for the API credentials, your phone
number, the login code, and your two-step verification password if enabled.
Secret prompts require an interactive terminal and hide input.

In the TUI, select a contact and press **`t`**. Its Telegram username or phone
from the note is prefilled; you can type a different username, profile URL, or
international phone number. Press Enter to look up the account, then Enter again
to confirm the person and save their current Telegram details to the note and
the stable account link. Escape cancels. Phone lookup depends on Telegram's
privacy settings. Login is needed once; you can link contacts as you use Wassup.

Linked rows show a small indicator:

| Indicator | Meaning |
| --- | --- |
| `✈✓` | Account verified |
| `✈~` | Account verified, but the note's username or visible phone is outdated; `t` updates it |
| `✈?` | Saved link not yet checked, or verification failed (for example, offline) |
| `✈!` | Account deleted or unavailable |

Check results are saved locally. When the TUI opens or you press `r`, it checks
only links whose configured interval has elapsed (14 days by default). Recently
linked contacts also retain their verification date. Set the interval in config:

```json
"telegram": { "check_every_days": 14 }
```

Use `0` to disable automatic checks. Automatic checks run while opening/reloading
Wassup; there is no background system timer. Failed attempts also defer automatic
retry by this interval, preventing repeated retries on every launch.

Press **`v`** to check the selected contact or **`V`** to check all linked contacts
immediately, regardless of the interval. An animated indicator shows the current
phase (connecting, authorizing, checking accounts, or saving), elapsed seconds,
and completed/total contacts. Escape cancels the active request. Network timeouts
stop with an actionable error and preserve earlier results. Connection,
authorization, and individual request waits have separate 20-second limits.

The preview shows the last check, next automatic check, and any failed attempt.
Checks use the saved user ID, so a renamed
username or changed phone never silently switches the link to a different person.
Checks also fetch visible birthdays and phone numbers and fill empty note fields.
Existing birthday and phone values are preserved; `t` still explicitly confirms
identity changes. A hidden birthday or phone never clears a saved value. Birthdays
with a hidden year use `--MM-DD`; otherwise they use `YYYY-MM-DD`. Telegram privacy
settings determine which fields are available. All profiles are fetched before
import begins, and the preview refreshes afterward. Press `V` once to import for
previously checked links without waiting for their next scheduled check.

For the optional command-line linking walkthrough, run `wassup telegram link`.
It shows suggestions using `cnt:telegram`, `cnt:phone`, and names. Enter a
username, a user ID displayed by the command, or `/Alex` to search your Telegram
contacts and private chats (including archived chats). Press Enter to skip a
note; each interactive selection needs confirmation. Previously linked notes
are skipped. To link or replace a single mapping explicitly:

```sh
wassup telegram link --note 'Alex.md' --user '@alex'
```

Links store stable Telegram user IDs, so changes of username do not change who
is synced. A Telegram account can only be linked to one note in a given config.
After renaming or removing a note, remove its old entry from `links.json` before
linking the same person to a new path.

Preview reports proposed birthday, phone, and date changes without editing notes.
Sync fills missing birthdays and phones even when there are no outgoing messages,
and advances the configured last-contact field, preserving existing profile values,
newer dates, snoozes, intervals, and note content. All linked histories and profiles
are fetched before writes begin; a
Telegram API error aborts that fetch phase without updating any notes. Invalid
notes are skipped with warnings. A local write failure reports partial progress.
Dates use the machine's local timezone. Only cloud personal chats are supported;
bots, groups, channels, Saved Messages, and existing Secret Chats are excluded.
Message text is not saved. This command is manual; no automatic schedule is installed.

Credentials, session data, links, and the `checks.json` validation cache live beside the selected config in
`<config-path>.telegram/` (normally `~/.config/wassup/config.json.telegram/`).
Files use mode `0600`, the directory uses `0700`, and a lock prevents concurrent
Telegram commands. The integration only reads Telegram data, but the session
itself grants account access: keep it out of shared folders and version control.
To disconnect, revoke the Wassup session in Telegram's **Settings → Devices**.
An expired session requires `wassup telegram login` again. Incorrect API
credentials can be corrected in that directory's `credentials.json`.

Global overrides go before the subcommand:

```sh
wassup --config /path/to/config.json --contacts /path/to/notes telegram sync --dry-run
```

## Optional notifications

Notifications require Linux, a systemd user session, and `notify-send`. The TUI
itself does not require systemd.

```sh
wassup --install-notifications
```

This installs a persistent daily user timer—no server or background Wassup
process. Run `wassup --notify` to trigger the check manually. Successful sends
are recorded once per local day to avoid duplicates.

## Keys

| Key | Action |
| --- | --- |
| `j`, `k`, arrows | Move |
| `tab`, `]` / `shift+tab`, `[` | Next / previous view |
| `/` | Search names and notes |
| `enter` | Toggle preview |
| `o` | Open the selected note |
| `t` | Link or update Telegram for the selected contact |
| `v` | Verify the selected contact's Telegram link |
| `V` | Check all linked Telegram contacts now |
| `a` | Set the contact interval |
| `x` | Remove from Keep in touch |
| `d` | Set last-contacted date; blank means today |
| `s` | Snooze |
| `r` | Reload notes |
| `esc` | Close preview or clear search |
| `q` | Quit |

The preview starts open and moves beside the list on terminals at least 100
columns wide. Removing a contact from **Keep in touch** preserves the note and
last-contact date; only the interval and snooze are removed.

## File safety

- Updates use a temporary file and atomic rename.
- Unrelated frontmatter, Markdown content, permissions, and line endings are
  preserved.
- Invalid notes are skipped with a warning; valid notes continue to load.
- Malformed or unknown config values are reported instead of overwritten.
