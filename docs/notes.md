# Notes

A Notes page in the app: quick capture like Google Keep, a calm writing
surface like Notion, and AI help on demand. Every note is an ordinary
Markdown file, so nothing about a note lives anywhere but in that file.

## What we took from where

| From | Idea | In NeoCloud |
| --- | --- | --- |
| Google Keep | "Take a note…" bar, cards in a grid, pinned on top, colors, labels, checklists, archive | Capture bar on the Notes page, card grid (pinned first), 8 colors, tags, `- [ ]` checklists you tick on the card and in the note, archive |
| Notion | One quiet page per note, big title, `/` commands, AI on the selection or the page (summarize, action items, improve, continue) | Note editor with a title field, a formatting bar and `/` commands, an AI menu that works on the selection or the whole note |
| Obsidian | Notes are plain `.md` files with YAML properties; folders are notebooks | Frontmatter for tags, pin, color, archive; subfolders of `Notes` are notebooks |
| Apple Notes / Keep | Autosave, no Save button | Saves a moment after you stop typing; the version history keeps the older text |

Left out on purpose: databases and nested pages (Notion), reminders and
drawing (Keep), backlinks/graph (Obsidian). They would need state outside
the file or a second editor model; they can come later without changing the
file format.

## Files

Notes live in the account's `/Notes` folder (made when the first note is
saved). A subfolder of `/Notes` is a notebook; notes in `/Notes` itself are in
no notebook. A note's file name is its title plus `.md`.

```markdown
---
tags:
  - work
  - ideas
pinned: true
color: amber
archived: false
---
Meeting with the team

- [x] Book the room
- [ ] Send the agenda
```

- Frontmatter holds only what the Notes page needs: `tags` (list of text),
  `pinned`, `archived` (true/false) and `color` (`default`, `red`, `orange`,
  `amber`, `green`, `teal`, `blue`, `purple`, `gray`). Unknown keys are kept
  as they are when the note is saved, so a note written in Obsidian keeps its
  own properties. A note without frontmatter is a plain note.
- The body is Markdown: headings, lists, `- [ ]` checklists, quotes, code,
  `---`, `**bold**`, `*italic*`, `` `code` ``, links.
- Because a note is a file it syncs, shows in search, has versions, goes to the
  trash and can be shared like any other file. Notes in a vault never show.
- The parser (`server/utils/note_markdown.js`) reads a simple YAML subset
  (scalars and lists of scalars) and writes the same subset back.

## Server

`server/services/notes/`:

- `store.js` lists, reads, creates and updates notes on top of the agent file
  services (`services/agent/files.js`): paths, revisions, versions and the
  trash all come from there.
- `cache.js` keeps the parsed card of each note (title, snippet, tags, …) in
  memory, keyed by node id and etag, so the list doesn't decrypt every note
  each time. Nothing about a note's content is written to the database.
- `ai.js` runs the AI actions through the AI facade (`ai/client.js`) after
  `assertAiAllowedForNode`. AI never writes a note: it returns text and the
  editor inserts it where you choose.

Routes (`server/routes/notes.js`, session or phone app): see
[api.md](api.md#notes).

## AI

All on demand from the note's AI menu, on the selection when there is one,
else the whole note:

| Action | Result |
| --- | --- |
| Summarize | A short paragraph, inserted at the top or copied |
| Action items | A `- [ ]` checklist of the to-dos in the note |
| Improve writing | The same text, clearer, same language |
| Make shorter | The same text, shorter |
| Continue writing | A few sentences that follow on |
| Suggest title | A title; tapping it renames the note |
| Suggest tags | Up to five tags, preferring the ones you already use |
| Ask | A free question about the note; the answer can be inserted |

The menu shows only while AI is on for the account (Settings › AI) and
`summaries` is switched on there.

**Titles for untitled notes.** A note started without a title gets one made
up by NeoCloud (its first line, or "Note <date>"). Once such a note holds a
few sentences and is saved, the editor asks AI for a title once and shows it
under the title field; "Use title" renames the note, nothing changes until
then.

## App

`flutter_app/lib/main_notes*.dart` is the UI, `lib/src/notes/` the logic:

- `note_models.dart` — a note card, a full note, the AI actions.
- `note_markdown.dart` — blocks for the read view, checklist ticks, and the
  text edits of the formatting bar, the `/` menu and Enter in lists; pure,
  tested (`test/notes_markdown_test.dart`).
- `note_session.dart` — one open note: autosave after typing stops, one save
  at a time, each with its base revision; a conflict asks before saving over.

UI: `main_notes.dart` (page, filters, capture bar, card grid),
`main_notes_editor.dart` (editor, formatting bubble, phone bar, `/` menu),
`main_notes_widgets.dart` (cards, colors, read view, Markdown styling while
typing), `main_notes_ai.dart` (AI menu and answers).

The page: search, notebook and tag filters, Archive; the capture bar; pinned
cards, then the others. A note opens in the editor (beside the list on wide
screens, full screen on phones): title, color, pin, tags, notebook, the AI
menu and an Edit/Read toggle. Markdown is typed as text and shown styled.
Selecting text shows a formatting bubble above it (bold, italic,
strikethrough, code, link, Ask AI); on phones the formatting bar sits above
the keyboard. `/` at the start of a line opens the blocks (checklist,
headings, lists, quote, code, divider, Ask AI). Checklists tick in the read
view.
