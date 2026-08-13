# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```sh
go build -o maily . && ./maily   # build + run
go run .                         # run directly (needs ~/.config/maily/credentials.json)
go vet ./...                     # vet
go test ./...                    # no test files exist yet
go test ./ui -run TestName       # single test, once tests exist
```

Running the app requires real Gmail OAuth credentials at `~/.config/maily/credentials.json` and an interactive browser consent on first run; it takes over the terminal (alt screen). Prefer `go build` / `go vet` for verification.

## Architecture

Three packages behind `main.go`, which wires them in order: `auth.Client(ctx)` → `gmail.New` → `ui.New` → `tea.NewProgram(ui.Root(model))`.

- **`auth/`** — OAuth2 with the user's own Google client secret. Spins a `127.0.0.1:0` HTTP listener for the callback (port assigned at runtime), caches the token at `~/.config/maily/token.json` (0600). Scope is `gmail.modify`. A failing refresh silently re-runs the interactive flow.
- **`gmail/`** — thin wrapper over `google.golang.org/api/gmail/v1`. `MessageSummary` (list rows) and `FullMessage` (reader). `ListInbox` lists IDs, then fetches metadata for each concurrently with a 15-slot semaphore, writing into an index-positioned slice so order is preserved; per-message errors leave a nil that gets dropped. Part bodies are base64-decoded then transcoded to UTF-8 from the part's `Content-Type` charset (Gmail hands back the original bytes, so this is what keeps windows-1252 mail from being mojibake).

**Body source is a choice, not a fallback.** `preferPlain` picks the sender's `text/plain` part over the converted HTML when it carries at least half as many non-URL words (`visibleWords`), and records it in `FullMessage.PlainText`. Reason: HTML mail is table-laid-out, and converting a table gives one cell per line — the sender's plain part keeps columns they aligned by hand. The half-words ratio is the knob for mail that picks wrong. Measured over 40 inbox messages it splits ~50/50, with stub plain parts ("view in browser", verification codes) correctly staying on HTML.

HTML bodies go through `html-to-markdown` then `cleanupMarkdown` (strips images, empty/self-referential links) — the reader depends on markdown links surviving that pass. `<img>` URLs are scraped separately into `FullMessage.Images` for the `I` keybind. The `table` plugin was tried and does nothing for real mail: it skips tables whose cells contain newlines, which layout tables always do.
- **`ui/`** — Bubble Tea. Everything else.

### UI message flow (the important part)

`ui.Model` is the only real `tea.Model`, but it is wrapped: `Root()` returns a `rootModel` whose `Update` first calls `Model.updateExtra`, which handles `replyLoadedMsg` and `editBodyDoneMsg`, and only falls through to `Model.Update` if unhandled. This exists because those two need to return commands from a value receiver in a way the main switch didn't accommodate. **New async messages normally belong in `Model.Update`'s switch; only add to `updateExtra` if you hit the same shape.**

Sub-models (`InboxModel`, `ReaderModel`, `ComposeModel`, `StatusBarModel`, `HelpModel`) are **not** `tea.Model`s — plain structs with pointer-receiver mutators and an `Update(tea.Msg) tea.Cmd`. `Model.Update` routes only *non-key* messages to them; keys go `handleKey` → `handleInboxKey` / `handleReaderKey` / `handleComposeKey` / `handleSearchKey` based on `AppState`. State transitions go through `m.enter(state)`, which also updates the status bar, help context, and hint line — don't set `m.state` directly (the help overlay's return path is the one exception, since it restores `prevState`).

### Mailbox buckets and polling

`Model.buckets [2]mailboxBucket` is indexed by the `Mailbox` enum (`MailboxInbox`, `MailboxSent`); adding a mailbox means widening that array. Each bucket holds messages, a `seen` map, and a `loaded` flag. A 10s `pollTickMsg` refetches both mailboxes (skipped while composing/searching, and always rescheduled first so an error can't kill the loop). New-mail detection is `!bucket.seen[id]`; the seen map is populated on first load, which is why startup never notifies. Notifications shell out to `notify-send`.

Fresh inbox mail is scanned for a login code by `extractCode` (`ui/code.go`), which reads only the subject and snippet — no extra API call, since the snippet is the first ~200 chars of the body. A code is copied to the clipboard with `wl-copy` and leads the notification. **The bar for a match is deliberately high** (a context word in en/pt plus a 4-8 digit or mixed alphanumeric token, rejecting years and all-letter tokens like `SCALEFY`) because a false positive silently overwrites the user's clipboard. `ui/code_test.go` holds real subject/snippet pairs for both directions.

Tab/Shift+Tab switch mailboxes from cache via `showCachedMailbox` — no refetch. `effectiveQuery()` = mailbox query prefix + active search query.

### Reader vim engine

`renderBody` has two paths: markdown (from HTML) goes through glamour, `PlainText` bodies only get `wrap`. **Never send a plain body through glamour** — it reads it as markdown and reflows hand-aligned columns into one paragraph.

`reader.go` is a hand-rolled vim layer over the rendered body: body → wrapped `lines []string`, cursor as (line, col) in **runes**, visual char/line selection, and text objects (`SelectWord`, `SelectQuote`, `SelectBracket`, `SelectParagraph`).

Multi-key sequences use a pending-key string on `ReaderModel` (`SetPending`/`Pending`/`ClearPending`) driven by `handleReaderPending` in `model.go` — states are `"y"`, `"yi"`, `"ya"`, `"i"`, `"a"`. Text-object nouns are mapped in one place, `applyTextObject`; a new noun goes there. A new motion is a `ReaderModel` method plus a case in `handleReaderKey`.

`composeView` renders the body cell by cell to paint the cursor, selection, and links. **Cursor/selection indices are runes; the width budget is cells** — emoji and CJK are two cells, combining marks zero. Mixing the two overflows the viewport width and wraps every following line (this was a real bug). Any new per-cell rendering must keep `runewidth.RuneWidth` in the width accounting; `ui/reader_test.go` guards it.

Images (`I`) go to kitty's `icat` kitten via `tea.ExecProcess` when `isKitty()`, which downloads and draws them itself; other terminals fall back to `openExternal`. Inline-in-frame graphics were not attempted — bubbletea's frame diffing and the kitty graphics protocol don't compose without unicode placeholders.

Links: `stripMarkdownLinks` pulls `[text](url)` out before wrapping, then `mapMarkdownLinks` re-attaches each to per-line `linkRange`s so `composeView` can emit OSC 8 hyperlinks and `LinkUnderCursor` can resolve `o`. Yank goes to `wl-copy` (Wayland only).

### Compose

The body is **not** a textarea — it's a plain `string` field edited externally. `w` while the body is focused (and reply from the reader, automatically) runs `openEditorCmd`, which writes a temp `.md`, `tea.ExecProcess`es `$EDITOR` (default `nvim`), and returns `editBodyDoneMsg`. Only `To` and `Subject` are `textinput`s. Replies carry `In-Reply-To`, `References`, and `ThreadID` so Gmail threads them.

### Theme

Single theme, grayscale. Raw hex constants and the prebuilt `lipgloss` styles both live in `ui/theme/theme.go`; the README documents the palette. Requires 24-bit color.

Surface colors are the **empty string on purpose** — `lipgloss.Color("")` resolves to nil and emits no escape, so the terminal's own background (and its transparency) shows through. Never "fix" a surface constant to `#000000`; that paints an opaque black over a transparent terminal. Only the cursor row (`ColorSurfaceBright`) and status pills are filled, and text on those uses `ColorInk`.
