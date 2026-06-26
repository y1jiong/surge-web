<div align="center">

# Surge Web

**Web-based dashboard for the Surge download manager**

[![Go Version](https://img.shields.io/github/go-mod/go-version/y1jiong/surge-web?style=flat-square&color=cyan)](go.mod)
[![License](https://img.shields.io/badge/License-Apache%202.0-grey.svg?style=flat-square)](LICENSE)

[Installation](#installation) • [Usage](#usage) • [Building](#building)

</div>

---

## What is Surge Web?

Surge Web provides a **browser-based interface** to control a running [Surge](https://github.com/SurgeDM/Surge) instance. Instead of using the terminal TUI, you can add, pause, resume, and delete downloads from any device on your network.

It acts as a **lightweight proxy** between your browser and Surge's HTTP API, with an embedded frontend served as a single binary.

---

## Features

- **Zero-config discovery** — automatically detects a running Surge instance via XDG runtime directories
- **Real-time progress** — SSE streaming for live download speed, ETA, and progress bars
- **Custom headers** — supply cookies, auth tokens, or referrers for protected download URLs
- **File management** — delete downloads with optional file removal, serve completed files via browser
- **Global rate limit** — set bandwidth limits directly from the web UI
- **Single binary** — frontend embedded with Go's `embed`, no external assets needed
- **Dark theme** — GitHub-style color scheme, clean and minimal

---

## Installation

### Prebuilt Binary

Download the latest binary from [Releases](https://github.com/y1jiong/surge-web/releases).

### Go Install

```bash
go install github.com/y1jiong/surge-web@latest
```

Requires Go 1.25+.

---

## Usage

```bash
# Auto-discover Surge running locally
surge-web

# Connect to a specific Surge instance
surge-web --surge-host 192.168.1.100 --surge-port 1700 --token <token>

# Custom port
surge-web -p 9090
```

Then open `http://localhost:1799` in your browser.

### Flags

| Flag | Short | Default | Description |
|---|---|---|---|
| `--port` | `-p` | `1799` | Web UI listen port |
| `--surge-host` | `-H` | auto-detect | Surge server address |
| `--surge-port` | `-P` | auto-detect | Surge server port |
| `--token` | `-t` | auto-detect | Surge API token |

Authentication tokens are auto-discovered from `$XDG_STATE_HOME/surge/token` or the `SURGE_TOKEN` environment variable.

---

## Building

```bash
git clone https://github.com/y1jiong/surge-web.git
cd surge-web
go build -o surge-web .
```

The Surge submodule (`ref/Surge`) is only for API reference and is not required for building.

Cross-compilation is available via the included Makefile:

```bash
make linux-amd64    # Linux x86_64
make darwin-arm64   # macOS Apple Silicon
make windows-amd64  # Windows x86_64
```

---

## License

[Apache 2.0](LICENSE) © 2026 y1jiong

Surge Web is not affiliated with the Surge project. Surge is licensed under MIT © 2025 Junaid Islam.
