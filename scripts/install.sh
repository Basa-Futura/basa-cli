#!/usr/bin/env bash
#
# Install the Basa CLI.
#
#   curl -fsSL https://raw.githubusercontent.com/Basa-Futura/basa-cli/main/scripts/install.sh | bash
#
# Downloading with curl rather than a browser matters: macOS quarantines
# anything a browser, Slack, Mail, or AirDrop writes to disk, and Gatekeeper then
# refuses to run it. A curl-fetched file carries no quarantine attribute, so this
# needs no code signing and no Apple Developer account.
#
# Environment:
#   BASA_INSTALL_DIR   where to put the binary (default: ~/.local/bin)
#   BASA_VERSION       install a specific version instead of the latest
#
set -euo pipefail

REPO="Basa-Futura/basa-cli"
BINARY="basa"
INSTALL_DIR="${BASA_INSTALL_DIR:-$HOME/.local/bin}"

# Colour only when attached to a terminal, so a piped log stays clean.
if [ -t 1 ]; then
  bold() { printf '\033[1m%s\033[0m' "$1"; }
  dim() { printf '\033[2m%s\033[0m' "$1"; }
else
  bold() { printf '%s' "$1"; }
  dim() { printf '%s' "$1"; }
fi

say() { printf '%s\n' "$*"; }
err() { printf '%s\n' "$*" >&2; }

fail() {
  err ""
  err "$(bold 'Install failed:') $1"
  if [ $# -gt 1 ]; then
    err "$2"
  fi
  exit 1
}

# --- preflight ---------------------------------------------------------------

command -v curl >/dev/null 2>&1 || fail "curl is required but not installed."
command -v shasum >/dev/null 2>&1 || command -v sha256sum >/dev/null 2>&1 || \
  fail "shasum or sha256sum is required to verify the download."

# --- platform ----------------------------------------------------------------

detect_platform() {
  local os arch
  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  arch=$(uname -m)

  case "$os" in
    darwin | linux) ;;
    *) fail "Unsupported operating system: $os" "basa ships for macOS and Linux." ;;
  esac

  case "$arch" in
    arm64 | aarch64) arch="arm64" ;;
    x86_64 | amd64) arch="amd64" ;;
    *) fail "Unsupported architecture: $arch" ;;
  esac

  printf '%s-%s' "$os" "$arch"
}

# --- version -----------------------------------------------------------------

# Resolve the latest release by following the /releases/latest redirect. Uses no
# authentication, which is why this repository has to be public.
#
# The HTTP status is captured separately from the redirect target so that "no
# such repository or no releases yet" and "the network is down" produce different
# advice — they need different actions from whoever is installing.
latest_version() {
  local response code url tag

  if ! response=$(curl -sSL -o /dev/null -w '%{http_code} %{url_effective}' \
      "https://github.com/${REPO}/releases/latest" 2>/dev/null); then
    fail "Could not reach GitHub." \
         "Check your network connection, or pin a version with BASA_VERSION."
  fi

  code="${response%% *}"
  url="${response#* }"

  # 404 means the repository itself is not visible: it does not exist, or it is
  # private and this request carries no credentials. It is NOT what a public
  # repository with no releases returns — see the tag check below — so this
  # branch never caught the "nothing published yet" case, and saying so here was
  # the reason the first public install went looking for "vreleases".
  if [ "$code" = "404" ]; then
    fail "Cannot see ${REPO} on GitHub." \
         "It may be private, or the name may be wrong. If you have access, build from source instead: https://github.com/${REPO}/blob/main/docs/INSTALL.md"
  fi

  if [ "$code" != "200" ]; then
    fail "GitHub returned HTTP ${code} looking for the latest release." \
         "If this persists, pin a version with BASA_VERSION."
  fi

  tag="${url##*/}"

  # A repository with no releases answers /releases/latest with a redirect to
  # the releases index — a 200 whose last path segment is "releases", not a
  # tag. So the shape of the tag is the test, not the status code: anything
  # that is not v<digits> means there is nothing to install yet, and saying so
  # beats building a download URL out of whatever word GitHub landed on.
  case "$tag" in
    v[0-9]*) ;;
    *) fail "No release has been published for ${REPO} yet." \
            "Once one exists this command will find it. Until then, build from source: https://github.com/${REPO}/blob/main/docs/INSTALL.md" ;;
  esac

  printf '%s' "${tag#v}"
}

# --- install -----------------------------------------------------------------

sha256_of() {
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    sha256sum "$1" | awk '{print $1}'
  fi
}

main() {
  local platform version base tmp asset expected actual

  platform=$(detect_platform)
  version="${BASA_VERSION:-$(latest_version)}"
  # Accept both "0.1.0" and "v0.1.0". latest_version already strips the
  # prefix; BASA_VERSION is whatever the operator typed, and the tag form is
  # the one they will copy from the releases page. Without this, v0.1.0 built
  # a /releases/download/vv0.1.0 URL and failed as "no such release".
  version="${version#v}"
  asset="${BINARY}-${platform}"
  base="https://github.com/${REPO}/releases/download/v${version}"

  say ""
  say "$(bold 'Installing basa') $(dim "v${version} (${platform})")"

  tmp=$(mktemp -d)
  # shellcheck disable=SC2064
  trap "rm -rf '$tmp'" EXIT

  say "  downloading"
  curl -fsSL "${base}/${asset}" -o "${tmp}/${asset}" \
    || fail "Could not download ${asset} for v${version}." \
            "Check that a release exists at https://github.com/${REPO}/releases"

  # Verify before making anything executable: a truncated or corrupted download
  # must never become a binary on PATH.
  #
  # This proves the download arrived intact, not that the release is genuine —
  # checksums.txt comes from the same release as the binary, so anyone who could
  # replace one could replace the other. SECURITY.md says so plainly and puts
  # signing out of scope; the distinction is worth keeping here too.
  say "  verifying"
  curl -fsSL "${base}/checksums.txt" -o "${tmp}/checksums.txt" \
    || fail "Could not download checksums.txt for v${version}."

  expected=$(grep " ${asset}\$" "${tmp}/checksums.txt" | awk '{print $1}' || true)
  [ -n "$expected" ] || fail "checksums.txt has no entry for ${asset}."

  actual=$(sha256_of "${tmp}/${asset}")
  if [ "$expected" != "$actual" ]; then
    fail "Checksum mismatch for ${asset}." "Expected ${expected}, got ${actual}. Not installing."
  fi

  mkdir -p "$INSTALL_DIR"
  chmod +x "${tmp}/${asset}"
  mv "${tmp}/${asset}" "${INSTALL_DIR}/${BINARY}"

  say "  installed to $(bold "${INSTALL_DIR}/${BINARY}")"
  say ""

  # Tell the operator plainly if it will not be on PATH — otherwise their next
  # command is "basa: command not found" and they have no idea why.
  case ":${PATH}:" in
    *":${INSTALL_DIR}:"*)
      say "$(bold 'Next:') basa auth login --env <name> --url <url>"
      ;;
    *)
      say "$(bold "${INSTALL_DIR} is not on your PATH.")"
      say "Add this line to your shell profile (~/.zshrc on a Mac), then open a new terminal:"
      say ""
      say "    export PATH=\"${INSTALL_DIR}:\$PATH\""
      say ""
      say "$(bold 'Then:') basa auth login --env <name> --url <url>"
      ;;
  esac
  say ""
}

main "$@"
