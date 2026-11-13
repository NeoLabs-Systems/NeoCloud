# AI in NeoCloud

NeoCloud can use language models for work on your files: names for files
called `IMG_2041.jpg`, tags, summaries, sorting suggestions. This page covers
the foundation every such feature stands on (where the models come from, who
may use them, what leaves the server, and what never does) and then the
features themselves. The endpoints are
in [api.md](api.md#ai--apiai-session-or-phone).

AI is **off** until two things are true:

1. An admin turned it on (Admin › AI, or `NEOCLOUD_AI_ENABLED=true`) and a
   model is available.
2. The person opted in (Settings › AI, setting `ai_opt_in`).

While the admin switch is off, NeoCloud asks no provider anything, not even
for its model list on a person's status page.

## Connecting NeoRouter

NeoRouter is the recommended way: one connection reaches every model of all
its providers, with NeoRouter choosing and falling back between them.

1. Admin › AI › Providers › **Connect NeoRouter**, enter its address
   (`https://router.example.com`, or `router.local:3333` on the home network).
2. NeoCloud starts NeoRouter's device sign-in (client `neocloud`, shown as
   `NeoCloud (<host name>)`) and opens NeoRouter's approval page. Compare the
   code shown in both places and approve; in NeoRouter you choose which kinds
   of models NeoCloud may use (free, plan, paid) and any budget.
3. NeoCloud polls for the key (every few seconds, up to 10 minutes) and
   stores it in `.env` (`NEOROUTER_BASE_URL`, `NEOROUTER_API_KEY`,
   `NEOROUTER_ROLES`).

The connection has **roles**: `chat`, `embeddings`, `transcription` and
`images`. Work of a role that is switched off goes to the other providers.
With NeoRouter connected, roles left on "auto" use its profiles: fast text
work goes to `auto:fast`, summaries to `auto:smart`, images, embeddings and
transcription to the key's default profile.

Anyone can also connect **their own** NeoRouter under Settings › AI. It is
stored like a personal key and wins over the server's for that person. A
personal NeoRouter must be on the public internet, unless it is the
server's own NeoRouter.

## Other providers

Admin › AI › Providers sets server-wide keys (kept in `.env`, shown only
masked):

| Provider | Settings |
| --- | --- |
| OpenAI | `NEOCLOUD_OPENAI_API_KEY`, optional `NEOCLOUD_OPENAI_BASE_URL` |
| Custom OpenAI-compatible (vLLM, LM Studio, LiteLLM, …) | `NEOCLOUD_OPENAI_COMPATIBLE_BASE_URL`, `NEOCLOUD_OPENAI_COMPATIBLE_API_KEY` |
| Anthropic | `NEOCLOUD_ANTHROPIC_API_KEY`, optional `NEOCLOUD_ANTHROPIC_BASE_URL` |
| Google (Gemini) | `NEOCLOUD_GOOGLE_AI_KEY` |
| OpenRouter | `NEOCLOUD_OPENROUTER_API_KEY` |
| Ollama | `NEOCLOUD_OLLAMA_URL`, e.g. `http://192.168.1.10:11434` |

An admin's addresses may point into the LAN. Ollama is off until its address
is set.

Admin › AI › Models lists every model the server's providers offer, with its
kind (chat, embedding, transcription), whether it reads images and its price
class. The admin can switch models off for everyone, decide whether models a
provider adds later start on or off, and grant a switched-off model to single
accounts.

### Bring your own key

Under Settings › AI people can add their own key for OpenAI, a custom
endpoint, Anthropic, Google, OpenRouter, or their own Ollama. A personal key
always wins over the server's for that provider. Keys are encrypted with the
server's secrets key before they are stored and are never shown again; "Test"
lists the provider's models without saving anything.

A personal base URL may not point at the server's own network: loopback,
private, link-local and similar addresses are refused, for the address
itself and for every address its name resolves to. The one exception is the
address the admin configured for that same provider.

## Choosing models

Each kind of work has its own setting, `auto` by default:

| Setting | Used for |
| --- | --- |
| `ai_model_text` | Fast text work: titles, tags, metadata, sorting decisions |
| `ai_model_smart` | Summaries, questions about a file |
| `ai_model_vision` | Photos, screenshots, scans (only models that read images) |
| `ai_model_embedding` | Similarity for sorting and semantic search |
| `ai_model_transcription` | Audio and video |

A role resolves to the person's chosen model if it is available to them, else
to the NeoRouter profile for the role, else to the best available model of
the right kind (cheap and recent for text work, the strongest for smart work),
else the feature answers `503 AI_MODELS_UNAVAILABLE`. `GET /api/ai/status`
shows what each role resolves to right now.

A failing call is retried on the same model for rate limits, overload and
network errors (three attempts, short backoff).

## Privacy: what leaves the server

- **Only what a feature needs, to the provider the role resolves to**: the
  text extracted from a file (or the image itself for the vision role), and
  the instructions for the task. Nothing else from the account.
- **Never vault content.** Vault files are end-to-end encrypted; the server
  can't read them and every AI feature refuses them in code
  (`AI_VAULT`).
- **Never anything inside a folder excluded from AI.** The owner marks a
  folder "Exclude from AI" (`PUT /api/ai/folders/:id/excluded`); it covers
  every subfolder and file below it (`AI_EXCLUDED`).
- **Shared items follow their owner.** A file shared with you is processed
  only under the owner's settings and exclusions, never yours.
- **Model lists.** Discovering models asks each configured provider for its
  list. Prices, release dates and image support are read from OpenRouter's
  public model list, without a key and without anything about your users;
  `NEOCLOUD_AI_REFERENCE_URL=off` skips that.
- **The usage log holds metadata only**: account, feature, provider, model,
  tokens, duration, success or an error code. Never a prompt, a file name or
  an answer.

Providers handle what they receive under their own terms. For nothing to
leave your network, use Ollama or a NeoRouter that only routes to local
models.

## Budgets

`NEOCLOUD_AI_MONTHLY_TOKENS` (Admin › AI) caps the tokens each account may
use per calendar month (UTC); empty means no limit. Admins can set a different
budget per account, `0` for none or back to the default. Once an account
reaches its budget, AI features answer `429 AI_BUDGET_EXCEEDED` until the 1st.
All of an account's calls count, its own keys included. Admin › AI › Usage
shows this month's tokens per account, feature and model.

## Features

Each feature has a switch in Settings › AI (`ai_features`):

| Switch | Default | What it does |
| --- | --- | --- |
| `names` | on | Suggests names for files with meaningless names |
| `folders` | on | Suggests a folder to move a file to |
| `tags` | on | Suggests tags; accepted ones show on the file and are searchable |
| `summaries` | on | "Summarize" on a file |
| `onUpload` | off | New files get suggestions by themselves, without being asked |
| `autoApplyHighConfidence` | off | High-confidence suggestions are applied at once (still undoable) |
| `newTags` | on | Tags beyond the ones the account already uses |
| `newFolders` | off | Moves into a folder that doesn't exist yet |

**File types** (Settings › AI › File types, setting `ai_skip_types`): whole
categories (images, videos, documents, spreadsheets, presentations,
archives, audio, code, other) or single formats can be left out. Files of
those types get no titles, tags, folders or summaries: they are not queued,
not read and not sent anywhere.

### Nothing changes without you

AI never renames, moves or tags on its own. Every result is a **suggestion**
in the review queue (Review), with a confidence tier: **high** (preselected,
"accept all high"), **medium** (listed for review) and **low** (stored but
hidden). Accepting runs the normal rename or move, so sync clients and open
apps see it like any other change; name clashes get ` (1)`. Every accepted
suggestion can be **undone** as long as the file is still as AI left it.
A suggestion whose file changed meanwhile (new content, another name or
folder, the trash) turns **stale** and can no longer be accepted.

With `autoApplyHighConfidence` on, high-tier suggestions are applied right
away and listed as "applied", undoable in the same way.

**No folder is made before you accept.** A move into a folder that doesn't
exist yet is only a suggestion until you accept it; then the folder is
created in the same step as the move (a move that fails leaves no folder).
Such a move is never high-tier, so neither auto-apply nor "accept all
high-confidence" takes it: you accept each one yourself. Undoing it puts the
file back and removes the folder again once it is empty. Turning
`newFolders` off turns pending ones stale.

**Cleaning up.** Review shows "N empty folders created by AI — Remove" when
folders AI made (accepted moves into a new folder) are empty. It also
offers "Remove empty folders" for any empty folder at the top level created
in the last seven days, listed by name for you to confirm first: folders AI
made before NeoCloud kept track can't be told apart from your own. Only
empty folders are removed, never a vault or a shared folder, and they go to
the trash.

### How work starts

- **On request**: "Suggest names / folders / tags" for chosen files, or a
  backfill over a folder ("find badly named files in Downloads").
- **On upload**, with `onUpload` on: every new file gets a job. The worker
  follows the change journal like the search indexer, and waits up to two
  minutes for the indexer to read a new file's text instead of reading it a
  second time.

Jobs run in the background, two at a time. A failing provider is retried
after 1, 4, 16 and 64 minutes before the job gives up. While the admin
switch is off, the person isn't opted in, or their monthly budget is used
up, their jobs wait (`GET /api/ai/jobs` says why) and nothing is queued from
uploads while AI is off.

### One call per file

Names, tags and folders share **one** structured call per file: the model
reports the document's date, issuer, subject, type, amount, currency, tags,
language, a one-line summary (and a caption for photos), each with a
confidence. NeoCloud trusts an issuer or amount less when it can't find it
in the text. The result is kept with the file (`aiMeta` on the node), so
asking for folders after names costs nothing more.

- **Documents** (PDF, Office, text): the text the search index extracted
  (decrypted through the index's own store), else the same extractor run
  in memory, at most about 12 000 characters, to the `text` model.
- **Images and screenshots**: OCR text from the index when it has some,
  else the picture itself, scaled to 1024 px, to the `vision` model. Without
  a vision model images are skipped.

### Auto-title

Only meaningless names are renamed: camera names (`IMG_`, `PXL_`, `DSC`,
`20260312_142233`, `WhatsApp Image …`), screenshots (`Screenshot …`,
`Bildschirmfoto …`), scanner output (`scan0001`, `SCN_`, `doc123`), UUIDs,
hex and random ids, timestamps, and generic names (`download (3)`,
`document`, `untitled`, `Unbenannt-1`, `invoice` …). A name that already
says something is left alone, unless you ask for that file.

The model never writes the name. NeoCloud renders it from your template
(`ai_naming_template`, default `{date} {issuer} – {subject}`; also
`{year}` `{month}` `{type}` `{amount}` `{currency}` `{original}`). Missing
values drop out with their separators; the result is made valid on Windows,
macOS and Linux, capped at 120 characters, keeps its extension, and gets
` (1)` when the folder already has that name.

The date is the document's own date if it has a plausible one, else the
photo's EXIF date, else a date in the old file name, else the modification
time. A date after the upload or before 1900 is not believed.

**Camera photos** (EXIF camera data and a camera name) are not renamed
unless you ask for them: they get tags and a caption, which search finds.

### Auto-sort

Restraint first: "keep it where it is" is the default, a move has to be
clearly better. Which folders are considered at all:

- A file at the top level or in an inbox-like folder (Downloads, Unsorted,
  Scans, Desktop, …) may go into any folder it may move to.
- A file in a folder you made stays in that folder's subtree: AI never
  suggests moving it out of your structure.
- **Photos and videos** only ever go into your existing photo tree (Photos,
  Fotos, Bilder, Camera Uploads, …), into the year and month folders when
  you already sort it that way, and never into a new folder. A photo that
  isn't at the top level stays where it is. Photographed documents (a
  receipt, a scan) are filed like documents.
- Documents are never filed into the photo tree.

Then, cheapest first:

1. **Your rules** (Settings › AI › Rules): type, issuer, extension or a name
   pattern → a folder. A rule that matches decides; no model is asked.
2. **Similarity** between the file's extraction and each folder's profile
   (its path and the names inside it), from the `embedding` model; folder
   profiles are embedded once and again only when they change. A clear
   winner decides.
3. Otherwise the `text` model picks among the ten closest folders, or keeps
   the file where it is. The prompt (`server/services/ai/organize/prompts.js`)
   gives a numbered list to choose from, makes "keep it here" the preferred
   answer when unsure, asks for a confidence, and shows a few borderline
   examples. Picks below 60 % confidence are not suggested.

Only folders that exist are suggested (unless `newFolders` is on), never a
vault, never a folder excluded from AI, and never across the border of a
shared folder: a move suggestion never changes who can see a file. After
you accept three moves of the same kind of document from the same issuer
into one folder, NeoCloud offers a rule for it.

**New folders** (`newFolders` on) are a last resort. A proposed name is
compared with your folders ignoring case, accents, singular and plural, and
across languages (Photos = Fotos = Bilder = Pictures = Images, Documents =
Dokumente = Unterlagen, Invoices = Rechnungen, Receipts = Belege =
Quittungen, …), then by embedding similarity of the names; a match means
your existing folder is used instead. A new name is generic ("Invoices",
not "Invoice from Telekom March 2026") and in the language your folders
are named in. A new folder is suggested only once three files of the same
job would go into it, and a job suggests at most three new folders.

### Tags and search

Accepted tags appear on the file (`aiTags`). Search finds files by their
tags and by the extracted issuer, subject, type and caption, like by name.

### Summaries

"Summarize" asks the `smart` model (images: `vision`) for a few sentences.
The summary is kept, encrypted like the search text, until the file's
content changes, and shows which model wrote it.

### What is sent, per feature

| Feature | Sent to the provider | Usage log feature |
| --- | --- | --- |
| Names, tags, folders (extraction) | File name, modification date, up to ~12 000 characters of text or one scaled image, the account's existing tag names | `title`, `tags` or `sort` |
| Folder similarity | Folder paths with up to 15 names inside each; the file's name and extracted fields; for a proposed new folder, its name and your folder names | `embed` |
| Folder choice | The file's name and extracted fields, up to ten folder paths | `sort` |
| Summary | File name and up to ~12 000 characters of text, or one scaled image | `summary` |
