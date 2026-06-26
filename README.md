<div align="center">

# Surge Web

**Web-based dashboard for the Surge download manager**

[![Go Version](https://img.shields.io/github/go-mod/go-version/y1jiong/surge-web?style=flat-square&color=cyan)](go.mod)
[![License](https://img.shields.io/badge/License-Apache%202.0-grey.svg?style=flat-square)](LICENSE)

[Installation](#installation) • [Usage](#usage) • [Docker Compose](#docker-compose) • [Encrypted Download](#encrypted-download) • [Building](#building)

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
- **Encrypted download** — encrypt completed files with a password before downloading, decrypt locally in-browser
- **Dark theme** — GitHub-style color scheme, clean and minimal
- **System service** — install as a daemon via `surge-web service install`
- **Docker Compose** — one-command deployment with Surge and Surge Web

## Installation

### Prebuilt Binary

Download the latest binary from [Releases](https://github.com/y1jiong/surge-web/releases).

### Go Install

```bash
go install github.com/y1jiong/surge-web@latest
```

Requires Go 1.26+.

### Docker Compose

```bash
curl -O https://raw.githubusercontent.com/y1jiong/surge-web/main/docker-compose.yml
docker compose up -d
```

Starts both Surge (headless server) and Surge Web. Downloads land in `./downloads`, Surge state in `./surge-config`.

#### HTTPS via built-in TLS

Put your certificate and key in a `./certs` directory, then add to `docker-compose.yml`:

```yaml
# surge-web service
volumes:
  - ./certs:/certs:ro
command: ["--tls-cert", "/certs/cert.pem", "--tls-key", "/certs/key.pem", "--port", "443"]
ports:
  - "443:443"
```

#### HTTPS via reverse proxy

Alternatively, put a reverse proxy (Caddy, nginx, Traefik) in front of surge-web — no extra flags needed, just proxy to `surge-web:1799`.

### System Service

```bash
surge-web service install                           # register with defaults
surge-web service install --port 9090 --token abc   # pass startup flags
surge-web service start                             # start the service
surge-web service stop                              # stop the service
surge-web service status                            # check running status
surge-web service uninstall                         # remove the service
```

---

## Usage

```bash
# Auto-discover Surge running locally
surge-web

# Connect to a specific Surge instance
surge-web --surge-host 192.168.1.100 --surge-port 1700 --token <token>

# Custom port with HTTPS
surge-web -p 9090 --tls-cert cert.pem --tls-key key.pem
```

Then open `http://localhost:1799` in your browser (or `https://` if TLS is enabled).

### Flags

| Flag | Short | Default | Description |
|---|---|---|---|
| `--port` | `-p` | `1799` | Web UI listen port |
| `--surge-host` | `-H` | auto-detect | Surge server address |
| `--surge-port` | `-P` | auto-detect | Surge server port |
| `--token` | `-t` | auto-detect | Surge API token |
| `--tls-cert` | — | — | TLS certificate file (enables HTTPS) |
| `--tls-key` | — | — | TLS private key file (enables HTTPS) |

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

## Encrypted Download

Surge Web supports **password-protected encrypted downloads** to keep file contents private during transfer. Files are encrypted with AES-256-CTR before leaving the server.

### Encrypt a file

1. Wait for a download to complete.
2. Click the **Encrypt** button beside the download entry.
3. Enter a password when prompted.
4. The encrypted file (`.enc`) will download to your device.

### Decrypt a file

1. Click the **Decrypt File** button in the toolbar.
2. Select or drag the `.enc` file into the drop zone.
3. Enter the same password used during encryption.
4. Click **Decrypt & Download** — the original file is restored and saved locally.

Decryption runs entirely in your browser via the Web Crypto API. No data leaves your device, and no additional tools are required.

> **Note:** The encrypted format (`SENC` v0x02) is specific to Surge Web. Files are encrypted using AES-256-CTR. Keys are derived from the password using SHA-256 with domain separation. The original filename is preserved inside the encrypted container.

---

## License

[Apache 2.0](LICENSE) © 2026 y1jiong

Surge Web is not affiliated with the Surge project. Surge is licensed under MIT © 2025 Junaid Islam.
