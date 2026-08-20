# basa

A command-line client for Basa. It reads the same data you can see in the browser, as you, with the
same permissions.

**Internal and unstable.** No compatibility guarantees. Commands and output may change without
notice.

- [Getting set up](#getting-set-up)
- [Using it](#using-it)
- [Output](#output)
- [Exit codes](#exit-codes)
- [Sessions and security](#sessions-and-security)
- [Development](#development)

## What it deliberately cannot do

It has no administrative capability, and it never will. No database access, no running arbitrary
code, no looking at other teams' data, no migrations, no deploys, no cloud resources. Everything it
can reach, you could already reach by logging into Basa in a browser.

If you need something it refuses to do, that is a conversation with an engineer, not a missing flag.

---

## Getting set up

Four steps, in order. Steps 1 and 2 need someone else; 3 and 4 are yours.

### 1. Get API access turned on

Someone at Basa enables **API access** for your account — it is a per-user setting in the Basa admin.
Until that happens, every command will tell you access is not enabled.

### 2. Install it

**Not yet available as a download.** Where the binary gets hosted is still an open decision, so for
now it is built from source, which needs Go. See [docs/INSTALL.md](docs/INSTALL.md) for the full
picture and why.

```bash
git clone https://github.com/Basa-Futura/basa-cli.git
cd basa-cli
make install          # builds and copies to ~/.local/bin/basa
basa version          # check it worked
```

### 3. Get a token

In Basa, open the settings menu and choose **API Tokens**. Create one with the **read** ability.

It is shown **once**. Copy it before you close the page.

### 4. Log in

```bash
basa auth login --env staging --url https://staging.basa.example
```

Paste the token at the prompt. It is not echoed, and it is never accepted as a command argument —
that would leave it sitting in your shell history.

The token goes into your macOS keychain. If no keychain is available it falls back to a file at
`~/.config/basa/credentials.json` readable only by you, and tells you it did that.

Check it worked:

```bash
basa auth status --env staging
```

---

## Using it

Run `basa` on its own at any time to see the available commands.

| Command | Answers |
|---|---|
| `basa deals list` | What is outstanding right now |
| `basa deals show <id>` | Where one deal stands |
| `basa me` | Who am I, what teams can I see, when does my token expire |
| `basa auth login` | Store a token for an environment |
| `basa auth status` | Same as `me`, phrased as a health check |
| `basa auth logout` | Remove the stored token from this machine |
| `basa version` | Which build this is |

### Which environment — always required

**Every command that talks to Basa needs `--env` (or `-e`). There is no default, on purpose.** A
tool that quietly assumes production is one typo away from trouble, so `basa` would rather ask than
guess — even when only one environment is configured. `basa version` and `basa --help` are the
exceptions: they never reach a server, so they never ask which one.

```bash
basa deals list --env staging
basa deals list -e staging

export BASA_ENV=staging      # or set it once for your shell
basa deals list
```

Your environments live in `~/.config/basa/config.json`. Tokens do not — they are never written
there.

### Which team

If you belong to one team, `basa` uses it. If you belong to several, pass `--team` (or `-t`) — by
name, by a unique part of the name, or by id:

```bash
basa deals list -e staging --team "Acme Agency"
basa deals list -e staging --team acme          # a unique fragment is enough
```

A fragment matching two of your teams is an error, not a coin flip. The team in use is printed above
every result, so you can always see which one you are looking at.

### Deals

```
$ basa deals list -e staging
staging · Acme Agency
ID     STAGE        PROJECT          BRAND      COUNTERPARTY  UPDATED
EfhxL  Contracting  Spring Campaign  Northwind  Sam Rivera    2026-08-19
gbHJd  Outreach     Spring Campaign  Northwind  Jordan Lee    2026-08-19
```

| Flag | Effect |
|---|---|
| `--stage` | `outreach`, `negotiation`, `contracting`, `execution` |
| `--project` | Only this project (a project id) |
| `--limit`, `-n` | How many to show, 1–100 (default 25) |

```bash
basa deals list -e staging --stage contracting
basa deals show EfhxL -e staging
```

If more deals match than are shown, it says so — a partial list is never left looking complete.

---

## Output

A table by default, for reading. JSON for scripting, with `--json`:

```bash
basa deals list -e staging --json | jq -r '.data[].counterparty.name'
```

**Only the table or the JSON goes to stdout.** Every message, warning, and error goes to stderr — so
a pipeline keeps working even when a command fails, and the JSON stays parseable either way.

## Exit codes

| Code | Meaning | What to do |
|---:|---|---|
| `0` | It worked | — |
| `1` | Something about the command was wrong | Read the message; it says what to fix |
| `2` | That thing does not exist, or you cannot see it | Check the id |
| `3` | You are not logged in, or your session ended | `basa auth login --env <name>` |
| `4` | You are not allowed to do that | Ask a Basa administrator for access |

`3` and `4` are separate because the fix is different: one you can do yourself, the other needs a
person.

## Sessions and security

**Sessions last 8 hours**, so you will log in roughly twice a day. That is deliberate — the token
sits on your laptop, and a short-lived one limits the damage if the laptop goes missing.

`basa auth logout` removes the copy on your machine. It does **not** revoke the token on the server.
If you think a token has been exposed, delete it in Basa under **API Tokens**. If the whole account
is a concern, have an administrator turn off your API access — that kills every token you hold at
once.

Token issuance, use, and revocation are all logged on the server, by user and time. Token values are
never logged.

### Environment variables

| Variable | Effect |
|---|---|
| `BASA_ENV` | The environment to use, instead of `--env` |
| `BASA_TOKEN` | Use this token and ignore stored credentials entirely. This is what scripts and CI use |
| `BASA_NO_KEYRING` | Set to anything to force file storage instead of the keychain |
| `XDG_CONFIG_HOME` | Move the config directory off `~/.config` |

---

## Development

Requires Go 1.26. `.tool-versions` pins it for asdf and mise.

```bash
make check        # fmt-check + vet + test — run this before committing
make test-race
make build
make build-all    # cross-compile to dist/, with checksums
make help         # list every target
```

### How it fits together

This repository is only the client. The API it talks to lives in the **basa-web** repository, and
the two meet at HTTP and nowhere else — this module imports nothing from the application.

| Where | What |
|---|---|
| `basa-web` → `docs/cli/api.md` | The API reference: endpoints, gates, status codes |
| `basa-web` → `docs/cli/scope.md` | Scope of record for both halves: what is in, what is cut, and why |
| here → `docs/INSTALL.md` | Install, and the open hosting decision |

### Layout

```
cmd/basa/          entry point
internal/cli/      command tree; the one place an error becomes a message and an exit code
internal/commands/ one file per resource — flags, prompts, and formatting only
internal/client/   HTTP client for /api/v1; maps status codes onto the exit-code contract
internal/config/   environments and credential storage
internal/output/   table and JSON rendering; owns the stdout/stderr split
internal/fail/     the error type and the exit codes
```

Commands never build HTTP requests, and the client never formats output. Keeping that separation is
what stops domain logic leaking into a Go binary that has no business holding any.

## Licence

Uses [`github.com/basecamp/cli`](https://github.com/basecamp/cli) for credential storage —
MIT, Copyright 2025 37signals LLC.
