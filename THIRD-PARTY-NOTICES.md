# Third-party notices

The `basa` binary statically links the components below. Each is used under its own permissive
licence, reproduced or linked here as those licences require.

This file exists because of a specific obligation: MIT and BSD require their copyright and permission
notices to travel with "copies or substantial portions" of the software, and a compiled Go binary
contains that code. Publishing the notices with the source and the release satisfies it.

**None of these licences requires `basa` itself to be open source.** They are permissive, not copyleft
— see [LICENSE](LICENSE) §5. No GPL, LGPL, or AGPL component is present anywhere in the dependency
tree.

## Linked in every build

| Component | Licence | Copyright |
|---|---|---|
| [github.com/basecamp/cli](https://github.com/basecamp/cli) | MIT | Copyright 2025 37signals LLC |
| [github.com/spf13/cobra](https://github.com/spf13/cobra) | Apache-2.0 | Copyright 2013–2023 The Cobra Authors |
| [github.com/spf13/pflag](https://github.com/spf13/pflag) | BSD-3-Clause | Copyright 2012 Alex Ogier; Copyright 2012 The Go Authors |
| [github.com/zalando/go-keyring](https://github.com/zalando/go-keyring) | MIT | Copyright 2019 Zalando SE |
| [golang.org/x/term](https://golang.org/x/term) | BSD-3-Clause | Copyright 2009 The Go Authors |
| [golang.org/x/sys](https://golang.org/x/sys) | BSD-3-Clause | Copyright 2009 The Go Authors |

## Linked on Linux only

| Component | Licence | Copyright |
|---|---|---|
| [github.com/godbus/dbus](https://github.com/godbus/dbus) | BSD-2-Clause | Copyright 2013 Georg Reinke, Google |

Used by `go-keyring` for the Secret Service keyring backend.

## Linked on Windows only

| Component | Licence | Copyright |
|---|---|---|
| [github.com/danieljoos/wincred](https://github.com/danieljoos/wincred) | MIT | Copyright 2018 Daniel Joos |

Used by `go-keyring` for the Windows Credential Manager backend.

## Notes

**Apache-2.0 §4(d)** requires propagating the contents of a `NOTICE` file where the licensed work
includes one. Cobra ships no `NOTICE` file, so there is nothing to propagate beyond its licence text
and copyright, both recorded above.

**No component is modified.** `basa` imports each as a dependency and vendors none of it, so there are
no changes to state under Apache-2.0 §4(b).

## Regenerating this list

The set of linked components differs by platform, because the keyring backend does. To re-derive it:

```bash
for os in darwin linux windows; do
  echo "== $os"
  GOOS=$os GOARCH=amd64 go list -deps ./cmd/basa \
    | grep -E '^(github|golang|gopkg)' | cut -d/ -f1-3 | sort -u
done
```

Licence texts live in the module cache under `$(go env GOMODCACHE)`.
