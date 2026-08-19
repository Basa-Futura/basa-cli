# Installing basa

## Status: build-from-source only

**The one-line install does not exist yet, and this is the open decision blocking it.**

`basa` is a single self-contained binary — no PHP, no Docker, no Node, nothing to install alongside
it. That part is done. What is unresolved is *where the binary is hosted so a non-engineer can
fetch it*.

This repository is **private**, so an unauthenticated `curl` against a release asset does not work.
There are three ways out, and the choice has real tradeoffs:

| Option | What the operator does | Cost |
|---|---|---|
| **Host the binary at an unauthenticated URL the org controls** | Pastes one line into Terminal | Someone has to decide where |
| **Issue a read-only GitHub token** | Pastes one line that includes the token | Puts a long-lived secret on an unmanaged laptop |
| **Sign and notarize with an Apple Developer ID** | Downloads and double-clicks, any channel | New third-party relationship and an annual fee |

### Why a plain download is not enough

macOS quarantines anything downloaded by a browser, Slack, Mail, or AirDrop, and Gatekeeper then
refuses to run it. For a non-engineer that is a hard stop.

Files fetched with `curl` are **not** quarantined. So a one-line `curl` install sidesteps Gatekeeper
entirely, with no code signing and no Apple Developer account — which is why it is the recommended
path once hosting is settled. And since running a CLI means opening Terminal anyway, pasting one line
there is not an extra imposition.

---

## Until then: build from source

This needs Go, so it is an engineer's path, not the operator's.

```bash
# Go 1.26 — the repo pins it via .tool-versions
asdf plugin add golang && asdf install golang 1.26.6

git clone https://github.com/Basa-Futura/basa-cli.git
cd basa-cli
make install
```

`make install` builds `basa` and copies it to `~/.local/bin/basa`.

Check that directory is on your `PATH`:

```bash
echo $PATH | tr ':' '\n' | grep -q "$HOME/.local/bin" && echo "on PATH" || \
  echo 'add this to your shell profile: export PATH="$HOME/.local/bin:$PATH"'
```

Then:

```bash
basa version
```

## Building for someone else's Mac

```bash
make build-all
```

Writes `dist/basa-darwin-arm64`, `dist/basa-darwin-amd64`, the two Linux equivalents, and
`checksums.txt`.

Apple Silicon needs `darwin-arm64`; Intel Macs need `darwin-amd64`. If you hand one of these over
directly, the recipient hits the Gatekeeper problem above — which is exactly the thing the hosting
decision resolves.

## Uninstalling

```bash
rm ~/.local/bin/basa
rm -rf ~/.config/basa          # config and, if the keychain was unavailable, credentials
```

If credentials went to the macOS keychain rather than a file, remove them in Keychain Access by
searching for **basa**.
