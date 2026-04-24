# maily

A fast, keyboard-driven terminal UI for Gmail, built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) and [Lipgloss](https://github.com/charmbracelet/lipgloss).

Read, search, reply, compose, and triage your inbox without leaving the terminal.

## Features

- **Inbox** — zebra-striped table with per-cell cursor, unread emphasis, and flag column for attachments and read state.
- **Reader** — full-message view with scrollable body, header block, and attachment badges.
- **Compose** — plaintext compose with `To` / `Subject` / `Body` fields, focus cycling, and reply threading via `In-Reply-To` / `References`.
- **Search** — full Gmail query syntax (e.g. `is:unread from:boss@corp.com newer_than:7d`).
- **Trash** — one-key archive to Gmail trash.
- **Help overlay** — context-aware keybinding table on `?`.
- **Responsive layout** — all views recompute on terminal resize.
- **OAuth2** — standard Google OAuth client secret flow, token cached locally.

## Install

### From source

```sh
git clone https://github.com/davitostes/maily
cd maily
go install .
```

This puts `maily` on your `$GOPATH/bin` (usually `~/go/bin`). Make sure that's on your `PATH`.

### Build a standalone binary

```sh
go build -o maily .
./maily
```

Requires Go 1.23 or newer.

## Setup

maily uses your own Google OAuth 2.0 client credentials — you control the project and quota.

1. Go to the [Google Cloud Console](https://console.cloud.google.com/).
2. Create (or pick) a project and **enable the Gmail API**.
3. Under **APIs & Services → Credentials**, create an **OAuth 2.0 Client ID**:
   - Application type: **Desktop app** (simplest) or **Web application**.
   - If Web: add `http://127.0.0.1` as an authorized redirect URI (the port is assigned at runtime; Google matches on prefix).
4. Download the client secret JSON.
5. Save it to:

   ```
   ~/.config/maily/credentials.json
   ```

On first run maily will open your browser for consent, capture the callback on a local port, and cache the refresh token at `~/.config/maily/token.json` (mode `0600`).

Requested scope: `https://www.googleapis.com/auth/gmail.modify` (read, send, trash, label — no delete).

## Usage

```sh
maily
```

### Keybindings

**Inbox**

| Keys | Action |
|---|---|
| `↑` / `↓` or `j` / `k` | move cursor row |
| `←` / `→` or `h` / `l` | move cursor column |
| `g` / `G` | jump to top / bottom |
| `Enter` | open selected email |
| `c` | compose new message |
| `r` | reply to selected |
| `d` | move to trash |
| `/` | search (Gmail query) |
| `Esc` | clear active query |
| `R` | reload |
| `?` | toggle help |
| `q` / `Ctrl+C` | quit |

**Reader**

| Keys | Action |
|---|---|
| `j` / `k` or `↑` / `↓` | scroll body |
| `Space` / `PgDn`, `PgUp` | page scroll |
| `g` / `G` | top / bottom |
| `r` | reply |
| `d` | trash |
| `Esc` | back to inbox |

**Compose**

| Keys | Action |
|---|---|
| `Tab` / `Shift+Tab` | cycle fields |
| `Ctrl+S` | send |
| `Ctrl+D` / `Esc` | discard |

## Theme

maily ships with one built-in theme — a dark, high-contrast palette with lavender and cyan accents. Colors are defined in [`ui/theme/theme.go`](ui/theme/theme.go):

| Role | Hex |
|---|---|
| Background | `#050509` |
| Surface | `#0B0A12` |
| Surface Alt | `#110E1B` |
| Header BG | `#5A2E96` |
| Accent (cyan) | `#5EEBFF` |
| Accent Soft (lavender) | `#B58CFF` |
| Success | `#77F2C6` |
| Warning | `#FFCB6B` |
| Danger | `#FF8FA3` |
| Border | `#2B1D45` |
| Muted | `#8F96A8` |
| Foreground | `#F7F4FF` |

Terminals must support 24-bit true color for accurate rendering.

## Project structure

```
maily/
├── main.go                  entry point
├── auth/oauth.go            OAuth2 flow + token cache
├── gmail/client.go          Gmail API wrapper
└── ui/
    ├── theme/theme.go       palette and pre-built lipgloss styles
    ├── model.go             root Bubble Tea model and state machine
    ├── inbox.go             inbox list view
    ├── reader.go            full-message reader with viewport
    ├── compose.go           compose / reply form
    ├── statusbar.go         bottom status bar
    └── help.go              help overlay
```

## Files and directories

| Path | Purpose |
|---|---|
| `~/.config/maily/credentials.json` | Google OAuth client secret (you provide) |
| `~/.config/maily/token.json` | cached OAuth token (auto-created, mode `0600`) |

## Roadmap

- [ ] Label management (apply / remove)
- [ ] Thread view (conversation grouping)
- [ ] HTML rendering with soft line-wrapping
- [ ] Attachment download
- [ ] Multiple accounts
- [ ] Custom themes via config file

## Contributing

Issues and pull requests welcome. Please run `go vet ./...` and `go build ./...` before submitting.

## License

[MIT](LICENSE) © Davi Tostes
