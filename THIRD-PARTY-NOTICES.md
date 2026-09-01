# Third-party notices

The `basa` binary statically links the components below. Each is used under its own permissive
licence, and **the full text of every one is reproduced verbatim** in
[`internal/licenses/`](internal/licenses/).

Those texts are embedded in the binary. Run:

```bash
basa licenses
```

## Why verbatim, and why embedded

MIT and BSD do not merely require attribution. They require that the copyright notice **and the
permission and warranty text** accompany "copies or substantial portions" of the software, and a
statically linked Go binary is such a copy. A table of copyright lines does not satisfy that, and a
link satisfies it even less — a link is not inclusion, and it rots.

An earlier version of this file was a table of copyrights and repository links that claimed to
discharge the obligation. It did not. The texts now travel two ways: with the source, in
`internal/licenses/`, and inside the compiled artefact via `basa licenses` — which is the copy an
operator actually downloads.

**None of these licences requires `basa` itself to be open source.** They are permissive, not copyleft
— see [LICENSE](LICENSE) §5. No GPL, LGPL, or AGPL component is present anywhere in the dependency
tree.

## Linked in every build

| Component | Version | Licence | Copyright | Text |
|---|---|---|---|---|
| github.com/basecamp/cli | v0.2.1 | MIT | Copyright 2025 37signals LLC | [basecamp-cli.txt](internal/licenses/basecamp-cli.txt) |
| github.com/spf13/cobra | v1.10.2 | Apache-2.0 | Copyright 2013–2023 The Cobra Authors | [spf13-cobra.txt](internal/licenses/spf13-cobra.txt) |
| github.com/spf13/pflag | v1.0.10 | BSD-3-Clause | Copyright (c) 2012 Alex Ogier; Copyright (c) 2012 The Go Authors | [spf13-pflag.txt](internal/licenses/spf13-pflag.txt) |
| github.com/zalando/go-keyring | v0.2.7 | MIT | Copyright (c) 2016 Zalando SE | [zalando-go-keyring.txt](internal/licenses/zalando-go-keyring.txt) |
| golang.org/x/term | v0.45.0 | BSD-3-Clause | Copyright 2009 The Go Authors | [golang-x-term.txt](internal/licenses/golang-x-term.txt) |
| golang.org/x/sys | v0.47.0 | BSD-3-Clause | Copyright 2009 The Go Authors | [golang-x-sys.txt](internal/licenses/golang-x-sys.txt) |

## Linked on Linux only

| Component | Version | Licence | Copyright | Text |
|---|---|---|---|---|
| github.com/godbus/dbus/v5 | v5.2.2 | BSD-2-Clause | Copyright (c) 2013, Georg Reinke, Google | [godbus-dbus.txt](internal/licenses/godbus-dbus.txt) |

Used by `go-keyring` for the Secret Service keyring backend.

## Linked on Windows only

| Component | Version | Licence | Copyright | Text |
|---|---|---|---|---|
| github.com/danieljoos/wincred | v1.2.3 | MIT | Copyright (c) 2014 Daniel Joos | [danieljoos-wincred.txt](internal/licenses/danieljoos-wincred.txt) |
| github.com/inconshreveable/mousetrap | v1.1.0 | Apache-2.0 | Copyright 2022 Alan Shreve (@inconshreveable) | [inconshreveable-mousetrap.txt](internal/licenses/inconshreveable-mousetrap.txt) |

`wincred` is used by `go-keyring` for the Windows Credential Manager backend. `mousetrap` is pulled in
by `cobra`, which uses it on Windows to detect being launched from Explorer rather than a shell.

Every platform-specific notice is embedded in **every** build, not only the platform that links it.
Carrying nine short files costs a few kilobytes; a notice missing from the one build that needed it
costs more.

The set is pinned by a test — see [Regenerating this list](#regenerating-this-list). `mousetrap` was
absent from this file until that test existed, which is the whole argument for having it.

## Notes

**Apache-2.0 §4(d)** requires propagating the contents of a `NOTICE` file where the licensed work
includes one. Cobra ships no `NOTICE` file, so there is nothing to propagate beyond its licence text
and copyright, both recorded above and reproduced in full.

**No component is modified.** `basa` imports each as a dependency and vendors none of it, so there are
no changes to state under Apache-2.0 §4(b).

## Regenerating this list

The set of linked components differs by platform, because the keyring backend does. Resolve the set,
then the exact versions:

Which components link, per platform — this is what the tables above are grouped by:

```bash
for os in darwin linux windows; do
  echo "== $os"
  GOOS=$os GOARCH=amd64 go list -deps -f '{{if .Module}}{{.Module.Path}}{{end}}' ./cmd/basa \
    | grep -v '^$' | grep -v '^github.com/Basa-Futura/basa-cli$' | sort -u
done
```

Every module that links on any platform, with the version in use — the set this page has to
account for, and the set the test checks:

```bash
for os in darwin linux windows; do
  GOOS=$os GOARCH=amd64 go list -deps -f '{{if .Module}}{{.Module.Path}} {{.Module.Version}}{{end}}' ./cmd/basa
done | grep -v '^$' | grep -v '^github.com/Basa-Futura/basa-cli ' | sort -u
```

**Neither command carries a list of names, and that is deliberate.** An earlier version of the
second one filtered `go list -m all` through a `grep -E` alternation of the dependencies it expected
— which silently omitted `mousetrap` the moment it was added, so the step documented for finding
versions could not have found the version of the newest entry. Asking the build what links, rather
than telling it what to look for, has no such failure mode. (`go list -m all` is the wrong source
regardless: it reports 18 modules here, including test-only ones like `testify` and `go-spew` that
are never linked and need no notice.)

Copy each licence file out of `$(go env GOMODCACHE)` into `internal/licenses/`. Match on
`LICEN[CS]E*`, `MIT-LICENSE`, and `COPYING` — `basecamp/cli` names its file `MIT-LICENSE`, so a
pattern anchored only on `LICENSE` silently misses it and the notice goes missing.

Take the copyright line from the licence file itself, never from the repository's README or a previous
version of this table. Two entries here were wrong that way: go-keyring was recorded as 2019 when its
notice says 2016, and wincred as 2018 when its notice says 2014.

**The set is now pinned by a test rather than by remembering to run the above.**
`internal/licenses/licenses_test.go` compares the embedded `.txt` files against the modules `go.mod`
requires, and fails in both directions — a dependency with no notice, and a notice with no dependency.
It reads `go.mod` rather than resolving the per-platform graph on purpose: the policy on this page is to
carry every notice in every build, so the union of all platforms is the right question, and `go.mod` is
that union.

That test is the reason this list can be trusted. `mousetrap` — Windows-only, reached through `cobra` —
was missing from this file for exactly as long as the list was maintained by hand.
