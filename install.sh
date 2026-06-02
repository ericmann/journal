#!/bin/sh
# journal installer — downloads the latest release binary, verifies its SHA-256
# against the published checksums, and installs it on your PATH.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/ericmann/journal/main/install.sh | sh
#
# Safer (inspect first):
#   curl -fsSL https://raw.githubusercontent.com/ericmann/journal/main/install.sh -o install.sh
#   less install.sh && sh install.sh
#
# Environment overrides:
#   JOURNAL_VERSION=v1.4.1   install a specific tag (default: latest release)
#   PREFIX=$HOME/.local      install under $PREFIX/bin (default: /usr/local, else ~/.local)
#
# The whole script is wrapped in main() and only invoked on the final line, so a
# truncated download can never execute a partial script.
set -eu

REPO="ericmann/journal"
BIN="journal"

err() { printf 'install: %s\n' "$1" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

# fetch URL -> stdout, using curl or wget.
fetch() {
  if have curl; then curl -fsSL "$1"
  elif have wget; then wget -qO- "$1"
  else err "need curl or wget"; fi
}

# download URL FILE -> save to FILE.
download() {
  if have curl; then curl -fsSL -o "$2" "$1"
  elif have wget; then wget -qO "$2" "$1"
  else err "need curl or wget"; fi
}

# sha256 FILE -> hex digest, across the common tools.
sha256() {
  if have sha256sum; then sha256sum "$1" | awk '{print $1}'
  elif have shasum; then shasum -a 256 "$1" | awk '{print $1}'
  elif have openssl; then openssl dgst -sha256 "$1" | awk '{print $NF}'
  else err "need sha256sum, shasum, or openssl to verify the download"; fi
}

detect_os() {
  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  case "$os" in
    darwin|linux) printf '%s' "$os" ;;
    *) err "unsupported OS: $os (use Homebrew, a .deb/.rpm/.apk, or build from source)" ;;
  esac
}

detect_arch() {
  arch=$(uname -m)
  case "$arch" in
    x86_64|amd64) printf 'amd64' ;;
    aarch64|arm64) printf 'arm64' ;;
    *) err "unsupported architecture: $arch" ;;
  esac
}

# choose a writable install dir: $PREFIX/bin, else /usr/local/bin, else ~/.local/bin.
install_dir() {
  if [ -n "${PREFIX:-}" ]; then printf '%s/bin' "$PREFIX"; return; fi
  if [ -w /usr/local/bin ] 2>/dev/null; then printf '/usr/local/bin'; return; fi
  printf '%s/.local/bin' "$HOME"
}

main() {
  os=$(detect_os)
  arch=$(detect_arch)

  tag="${JOURNAL_VERSION:-}"
  if [ -z "$tag" ]; then
    tag=$(fetch "https://api.github.com/repos/${REPO}/releases/latest" \
      | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
    [ -n "$tag" ] || err "could not determine the latest release tag (set JOURNAL_VERSION)"
  fi
  version=${tag#v}   # GoReleaser archives are named without the leading 'v'

  tarball="${BIN}_${version}_${os}_${arch}.tar.gz"
  base="https://github.com/${REPO}/releases/download/${tag}"

  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT INT TERM

  printf 'install: downloading %s %s (%s/%s)...\n' "$BIN" "$tag" "$os" "$arch"
  download "${base}/${tarball}" "${tmp}/${tarball}" || err "download failed: ${base}/${tarball}"
  download "${base}/checksums.txt" "${tmp}/checksums.txt" || err "could not fetch checksums.txt"

  expected=$(grep " ${tarball}\$" "${tmp}/checksums.txt" | awk '{print $1}')
  [ -n "$expected" ] || err "no checksum listed for ${tarball}"
  actual=$(sha256 "${tmp}/${tarball}")
  [ "$expected" = "$actual" ] || err "checksum mismatch for ${tarball} (expected ${expected}, got ${actual})"

  tar -xzf "${tmp}/${tarball}" -C "$tmp" "$BIN" || err "could not extract ${BIN} from archive"

  dir=$(install_dir)
  mkdir -p "$dir" 2>/dev/null || err "cannot create install dir: $dir"
  if [ -w "$dir" ]; then
    install -m 0755 "${tmp}/${BIN}" "${dir}/${BIN}"
  elif have sudo; then
    printf 'install: %s is not writable; using sudo\n' "$dir"
    sudo install -m 0755 "${tmp}/${BIN}" "${dir}/${BIN}"
  else
    err "$dir is not writable and sudo is unavailable; set PREFIX=\$HOME/.local and re-run"
  fi

  printf 'install: installed to %s/%s\n' "$dir" "$BIN"
  case ":$PATH:" in
    *":$dir:"*) ;;
    *) printf 'install: note: %s is not on your PATH — add it to run %s\n' "$dir" "$BIN" ;;
  esac
  "${dir}/${BIN}" --version || true
}

main "$@"
