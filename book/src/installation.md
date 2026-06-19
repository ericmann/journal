# Installation

journal ships as a single static binary for macOS and Linux. Pick the method that works best for your setup.

## macOS — Homebrew (recommended)

```sh
brew install ericmann/tap/journal
```

Homebrew handles the quarantine flag automatically, so you won't see an "Allow Anyway" security prompt.

## macOS and Linux — install script

This script downloads the latest release, verifies its checksum, and installs it on your PATH:

```sh
curl -fsSL https://raw.githubusercontent.com/ericmann/journal/main/install.sh | sh
```

## Linux — packages

Each release includes `.deb`, `.rpm`, and `.apk` packages for amd64 and arm64:

```sh
sudo dpkg -i journal_*_linux_amd64.deb     # Debian / Ubuntu
sudo rpm  -i journal_*_linux_amd64.rpm     # Fedora / RHEL
sudo apk add --allow-untrusted journal_*_linux_amd64.apk
```

## Verify the installation

```sh
journal --help
```

You should see a list of available commands. If that works, move on to installing Ollama.

---

## Install Ollama (required for search)

journal uses [Ollama](https://ollama.com) to build the search index on your machine. This is what makes local semantic search possible — no API key, no data leaving your computer.

**macOS:**

```sh
brew install ollama
brew services start ollama   # runs Ollama as a background service
```

**Linux:**

```sh
curl -fsSL https://ollama.com/install.sh | sh
# Ollama is installed as a systemd service and starts automatically
```

### Pull the embedding model

Once Ollama is running, pull the model journal uses for indexing:

```sh
ollama pull qwen3-embedding:4b
```

This downloads a ~2.5 GB model. It only runs on your machine, and it only runs when journal is indexing or searching.

---

## Check everything is working

```sh
journal doctor
```

`journal doctor` checks that Ollama is reachable, the embedding model is present, and the search index is healthy. If something is misconfigured, it tells you exactly what to fix.

---

## Next step

Now that journal is installed, head to [Your First Day](first-day.md) to initialize a journal and capture your first note.

---

<details>
<summary>Build from source (Go 1.26+)</summary>

```sh
git clone https://github.com/ericmann/journal.git
cd journal
make install
```

This builds a version-stamped binary and installs it to `/usr/local/bin`. You can override the install prefix with `PREFIX=/your/path make install`.

</details>
