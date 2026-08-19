# basa

A command-line client for Basa. It reads the same data you can see in the browser, as you, with the
same permissions.

**Internal and unstable.** No compatibility guarantees. Commands and output may change without
notice.

---

## What it can do today

| Command | What it does |
|---|---|
| `basa auth login` | Store a Basa API token for an environment |
| `basa auth status` | Show who you are, and check your token still works |
| `basa auth logout` | Remove the stored token from this machine |
| `basa me` | Show your account, your teams, and your current token |
| `basa version` | Print the version |

That is the whole surface. Reading deals, contracts, and projects comes next.

## What it deliberately cannot do

It has no administrative capability, and it never will. No database access, no running arbitrary
code, no looking at other teams' data, no migrations, no deploys, no cloud resources. Everything it
can reach, you could already reach by logging into Basa in a browser.

## Getting set up

### 1. Turn on API access

Someone at Basa needs to enable **API access** for your account — it is a per-user setting in the
Basa admin. Until then, every command will tell you access is not enabled.

### 2. Install it

**Not yet available as a download.** Where the binary gets hosted is an open decision, so for now it
is built from source. See [docs/INSTALL.md](docs/INSTALL.md).

```bash
git clone https://github.com/Basa-Futura/basa-cli.git
cd basa-cli
make install          # builds and copies to ~/.local/bin/basa
```

### 3. Get a token

In Basa, open the settings menu and choose **API Tokens**. Create a token with the **read** ability.

It is shown **once**. Copy it before you close the page.

### 4. Log in

```bash
basa auth login --env staging --url https://staging.basa.example
```

Paste the token at the prompt. It is not echoed, and it is never accepted as a command argument —
that would leave it in your shell history.

The token is stored in your macOS keychain. If there is no keychain available it falls back to a
file at `~/.config/basa/credentials.json`, readable only by you, and tells you it did so.

## Which environment?

**Every command needs `--env`. There is no default, on purpose.** A tool that quietly assumes
production is one typo away from trouble, so `basa` would rather ask than guess — even when only one
environment is configured.

```bash
basa me --env staging
BASA_ENV=staging basa me      # or set it once for your shell
```

Your environments live in `~/.config/basa/config.json`. Tokens do not — they are never written
there.

## Output

A table by default, for reading:

```
$ basa me --env staging
Environment  staging
Name         Dana Reed
Email        dana@example.com
User ID      42
Teams        Acme Agency (7)
Token        basa-cli
Can          read
Expires      when the 8 hour session ends
```

JSON for scripting, with `--json`:

```bash
basa me --env staging --json | jq -r '.data.teams[].name'
```

Only the table or the JSON goes to stdout. Every message, warning, and error goes to stderr — so a
pipeline keeps working even when a command fails.

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

## Sessions expire after 8 hours

You will log in roughly twice a day. That is deliberate — the token sits on your laptop, and a
short-lived one limits the damage if the laptop goes missing.

`basa auth logout` removes the copy on your machine. It does **not** revoke the token on the server.
If you think a token has been exposed, delete it in Basa under **API Tokens** — and if the whole
account is a concern, have an administrator turn off your API access, which kills every token you
hold at once.

## Environment variables

| Variable | Effect |
|---|---|
| `BASA_ENV` | The environment to use, instead of `--env` |
| `BASA_TOKEN` | Use this token and ignore stored credentials entirely. This is what scripts and CI use |
| `BASA_NO_KEYRING` | Set to anything to force file storage instead of the keychain |
| `XDG_CONFIG_HOME` | Move the config directory off `~/.config` |

## Development

Requires Go 1.26 (`.tool-versions` pins it for asdf/mise).

```bash
make check        # fmt-check + vet + test — run this before committing
make test-race
make build
make build-all    # cross-compile to dist/, with checksums
```

The API it talks to is documented in the Basa web repo at `docs/cli/api.md`.

## Licence

Uses [`github.com/basecamp/cli`](https://github.com/basecamp/cli) for credential storage —
MIT, Copyright 2025 37signals LLC.
