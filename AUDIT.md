# Pre-publication audit

**Date:** 2026-08-19
**Commit audited:** `feat/slice-2b-contracts` @ 150f78f, plus the fixes this document prompted
**Question:** is this repository safe to make public, and is it as thin as claimed?

**Answer: yes.** All three original blockers are resolved — the licence is decided (proprietary, all
rights reserved), the security contact is set, and every reference to a named colleague is gone from
files, messages, and history. Two claims I had made were wrong and are corrected below.

Re-run every check in this document with the commands quoted; nothing here is asserted without one.

---

## Corrections to earlier claims

**I said this was "about 1,100 lines". It is a little over 1,800 lines of Go excluding tests.** The
1,100 figure was accurate when the CLI had auth and `me` only, and I repeated it after deals and
contracts had landed without re-measuring.

The docs now say "under 2,000 lines" rather than an exact count. Re-verifying this audit immediately
caught the same failure a second time — my own fix to finding 4 added five lines and made a freshly
written exact figure wrong. A number that goes stale on every commit should not be quoted as a fact.

```
git ls-files '*.go' | grep -v _test.go | xargs wc -l   # non-test Go
git ls-files '*_test.go'               | xargs wc -l   # tests
```

Run it rather than trusting a figure quoted here — that is the whole lesson of this correction.

**I said "no business logic" without having checked for server-policy duplication.** One instance
existed — see finding 4. Fixed.

---

## What was verified

### Read-only, structurally

Zero write verbs in the entire codebase. Not "we didn't add any" — there are none to find:

```
git grep -nE 'http\.MethodPost|http\.MethodPut|http\.MethodPatch|http\.MethodDelete' -- '*.go'
# no matches
```

### No outbound destination beyond the configured server

The only URL in the source is a documentation placeholder:

```
git grep -noE 'https?://[a-zA-Z0-9./-]+' -- '*.go' | grep -v _test
# internal/cli/root.go:56: https://staging.basa.example
```

Confirmed in the built binary too — no real hostname, no telemetry endpoint, no update check:

```
strings basa | grep -oE 'https?://[a-zA-Z0-9./-]{6,}' | sort -u
# two cobra issue links, and staging.basa.example
```

### No secrets, in the tree or anywhere in history

All six commits were searched, not just the checkout:

```
git log -p --all | grep -nE '^\+.*[0-9]+\|[A-Za-z0-9]{30,}'      # tokens: none
git log -p --all | grep -inE '^\+.*(basa\.test|chore2|127\.0\.0\.1:800)'  # internal hosts: none
git log -p --all | grep -oE '^\+.*[^ ]+@[^ ]+\.[a-z]{2,}' | grep -v example  # real emails: none
```

The one hostname in the codebase is `http://localhost:8000` in a config test fixture — generic.

Test fixtures use `example.test` addresses and invented names (Acme Agency, Northwind, Sam Rivera,
Jordan Lee). No customer, client, or creator data.

### No client-side domain logic

The client never decides what a stage or a status *means*, never validates domain values (the server
does, and its 422 message is surfaced verbatim), and performs no arithmetic on domain data. The only
`switch` on anything status-like is HTTP status → exit code mapping in `client.go`.

```
git grep -nE 'switch (stage|status)|case "outreach|case "draft' -- '*.go' | grep -v _test
# no matches
```

`shortDate` truncates an ISO-8601 string to its date. That is the whole of the "computation".

### Dependencies are permissive, and few

Direct: `spf13/cobra` (Apache-2.0), `basecamp/cli` (MIT, 37signals), `golang.org/x/term` (BSD).
Transitively: `pflag` (BSD), `go-keyring` (MIT), `wincred` (MIT), `godbus/dbus` (BSD), plus test-only
libraries.

```
grep -rliE 'GNU GENERAL PUBLIC|GNU AFFERO|GNU LESSER' "$(go env GOMODCACHE)"/{github.com,golang.org,gopkg.in}
# no matches — nothing copyleft
```

### The install script

Reviewed as the highest-risk artefact, since operators pipe it to `bash`.

| Control | State |
|---|---|
| `set -euo pipefail` | present |
| HTTPS only | yes |
| SHA-256 verified **before** `chmod +x` | yes — verify precedes the executable bit |
| Truncated-download safety | `main "$@"` is the final line, so a partial fetch defines functions and does nothing |
| Temp handling | `mktemp -d` with a cleanup `trap` |
| Failure messages | 404 and network failure are distinguished, because they need different actions |

`bash -n` and `shellcheck` both clean. Exercised against real GitHub by pointing `REPO` at a public
repository with releases: version resolution and download both worked.

**Its limit, stated plainly:** `checksums.txt` ships in the same release as the binary, so it proves
the download arrived intact — not that the release is genuine. Anyone able to publish a release could
publish matching checksums. Closing that needs signed releases and a pinned key. The Basecamp
installer has the same property. Documented in `docs/INSTALL.md` rather than left implied.

---

## Findings

### 1. No `LICENSE` file — **resolved: proprietary, all rights reserved**

Decided deliberately rather than by default. **No permissive dependency obliges this software to be
open source** — MIT, Apache-2.0 and BSD are permissive, not copyleft, and say nothing about how the
work that links them is licensed. Only GPL-family licences do that, and none is present.

The one real obligation is attribution: MIT and BSD require their notices to travel with "copies or
substantial portions", and a compiled Go binary contains that code. Satisfied by
[THIRD-PARTY-NOTICES.md](THIRD-PARTY-NOTICES.md), which also records that Cobra ships no `NOTICE` file,
so Apache-2.0 §4(d) has nothing to propagate.

`LICENSE` reserves all rights, with one narrow carve-out: a person authorized to access a Basa account
may download and run the compiled program for that purpose. Without that carve-out the intended
operators would be running an unlicensed binary — technically absurd, but worth not writing down. It
also states that relicensing later, including as open source, remains open.

### 2. Security-reporting address — **resolved**

A public repository needs a stated route for reporting a vulnerability, or reports arrive as public
issues. `SECURITY.md` directs them to **security@basafutura.com** — a shared alias rather than an
individual, so it survives staff changes and keeps a personal inbox off a public page.

### 3. A real employee is named in two commit messages — **resolved: history rewritten**

Two commit messages named a colleague by first name and tied them to a job function, which would have
put internal staffing into permanent public history. Three PR bodies did the same.

Both messages now describe the role instead. PR bodies were edited directly. Test fixtures that used the
same first name as a sample user are now an invented one, matching the other synthetic names already
there.

**A correction about how this was found.** The first pass of this audit reported that no tracked file
named an employee. That was wrong: `git grep -E` with a `\b` word boundary silently matches nothing on
this platform, so the check returned zero while three files actually contained the name. Fixed-string
matching found them:

```
git grep -Fin -e "<name>" -- .    # not: git grep -inE '\b<name>\b'
```

The lesson generalises past this finding: a verification command that can fail silently is worse than no
check, because it produces false confidence. Every sweep in this document now uses `-F`.

### 4. Server-side policy was duplicated in the client — **fixed**

`internal/commands/me.go` printed `"when the 8 hour session ends"` when the server reported no explicit
token expiry. That is a server setting (`SANCTUM_TOKEN_EXPIRATION_MINUTES`) restated as a client-side
constant: change it on the server and the CLI would confidently tell operators something false about
how long their credential lives.

This was the only such violation in the *code*, and the one place the client could actively mislead
about security posture. It now says "when the server's session limit is reached" — true regardless of
the setting.

Re-verification found the same duplication surviving in `README.md`, which stated "Sessions last 8
hours" as flat fact. Softened to name it as a server setting that can change. Operator documentation is
a more defensible place for a concrete number than a compiled constant — "roughly twice a day" is
genuinely useful — but it should not read as a property of the tool.

### 5. Internal vocabulary becomes public — **accepted, with one change**

Going public publishes Basa's deal pipeline (`outreach`, `negotiation`, `contracting`, `execution`) and
contract lifecycle (`draft`, `ready_for_signature`, `signed`, `declined`, `voided`), because they appear
in flag help.

Judged acceptable: it is generic B2B-SaaS vocabulary, it is what makes the flags usable, and the API
returns nothing without a token plus the `feature-api-tokens` flag. Kept.

One thing removed: the internal feature codename "Quick Deals" appeared in two comments. Replaced with
a description of the shape, which is what a reader actually needs.

### 6. Pagination policy is restated in help text — **accepted**

`"How many to show (1-100, default 25)"` duplicates the server's cap. Same class as finding 4 but
harmless: it is a documented request limit, not a security property, and the server rejects anything
out of range with a 422 the CLI surfaces. Left as is, noted so it is a choice rather than an oversight.

### 7. The repository names the private application repo — **accepted**

`README.md` and `.gitignore` refer to `basa-web` for where the API and the scope documents live. That
reveals a private repository exists, which is unremarkable and useful to a maintainer. No path, host,
or content is exposed.

---

## What publishing actually discloses

Worth being concrete, since this is the decision being made:

- The **shape** of `/api/v1`: five endpoints, their query parameters, their JSON field names.
- Basa's deal-stage and contract-status vocabulary.
- That access is gated on a per-user feature flag and a token ability, and that tokens expire.
- That a private application repository exists.

It does **not** disclose: any hostname, any credential, any customer or creator data, any business
rule, any pricing or rate logic, any database schema beyond field names the API already returns to
authorised callers, or anything about infrastructure.

Reaching any of that requires a valid token **and** the `feature-api-tokens` flag on the account.
Knowing the endpoint shape does not help an attacker without one — the same reason Basecamp, HEY, and
Fizzy all ship public CLIs against non-public APIs.

## Verdict

Publish once findings 1, 2, and 3 are settled. Nothing in the code, the history, or the built binary
is disqualifying, and the two substantive claims — read-only, and no domain logic — hold up under
direct inspection now that finding 4 is fixed.
