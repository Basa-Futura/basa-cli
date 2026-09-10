# Security

## Reporting a vulnerability

Please **do not** open a public issue for a security problem.

Email **security@basafutura.com** and expect an acknowledgement within two working days.

A shared alias is used deliberately rather than an individual's address: it survives staff changes, and
it keeps one person's inbox off a public page.

## Scope

This repository is a command-line client. It contains no embedded credentials, no business logic, and
no customer data — see [AUDIT.md](AUDIT.md) for the evidence behind that claim.

It does handle one secret at runtime: the operator's own API token, which `basa auth login` stores in
the OS keyring, or in a `0600` file where no keyring exists. That token is the asset most worth
protecting here, so how it is stored, and anything that could leak it, is explicitly in scope below.

**In scope:**

- The installer (`scripts/install.sh`) — it is piped to `bash`, so it is the highest-risk artefact here.
- Credential handling: how tokens are stored, and anything that could leak one to disk, to a log, to a
  process list, or to shell history.
- Anything that would let the CLI reach data the operator could not already reach by logging into Basa
  in a browser.

**Out of scope for this repository:**

- The Basa API and application. Report those through the same private channel; they live elsewhere.
- The absence of signed releases. `checksums.txt` is published alongside the binary, so it proves the
  download arrived intact, not that the release is genuine. This is a known limitation, documented in
  [docs/INSTALL.md](docs/INSTALL.md), not a vulnerability report we need.

## What this tool can and cannot do

It authenticates as a normal application user and is constrained by the same authorization as the web
application. It has no administrative capability: no database access, no arbitrary code execution, no
cross-team reads, no migrations, no deploys, no infrastructure access. Every request is a `GET`.

A token is bounded by a server-side session limit and can be revoked two ways: individually, or by
clearing the account's API-access flag, which invalidates every token that account holds on the next
request.

## If a token is exposed

1. Revoke it in Basa under **API Tokens**.
2. If the whole account may be compromised, ask an administrator to turn off its API access — that
   kills every token it holds at once, without needing to find them.
3. `basa auth logout` only removes the local copy. It does **not** revoke anything server-side.
