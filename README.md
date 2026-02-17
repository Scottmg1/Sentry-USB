# SentryUSB

A modern, feature-rich USB drive manager for Tesla vehicles — built on the foundation of [TeslaUSB](https://github.com/marcone/teslausb), fully rebranded and revamped at [Scottmg1/Sentry-USB](https://github.com/Scottmg1/Sentry-USB).

## What is SentryUSB?

SentryUSB turns a Raspberry Pi (or compatible SBC) into a smart USB drive for your Tesla's dashcam system. It automatically archives recordings, serves a modern web UI, and can be fully configured through a browser — no SSH or config file editing required.

**Key features:**
- **Modern Web UI** — Dark glassmorphism design with sidebar navigation, real-time dashboard, multi-camera viewer, file browser, and live log tailing
- **Setup Wizard** — 9-step guided configuration covering WiFi, storage, archiving, keep-awake, notifications, security, and advanced settings — all from your browser
- **Go API Server** — Lightweight single-binary backend that cross-compiles for ARM, replacing nginx + CGI scripts
- **Multi-camera Viewer** — 6 layout options for synchronized playback of all Tesla camera angles
- **Archive Support** — CIFS/SMB, rsync, rclone (cloud), and NFS
- **Keep-Awake** — BLE, TeslaFi, Tessie, and Webhook methods
- **10+ Notification Providers** — Pushover, Discord, Telegram, Slack, Signal, Matrix, AWS SNS, IFTTT, Gotify, Webhooks

## Architecture

```
Browser (React SPA)  ←→  Go API Server (single ARM binary)  ←→  Shell Scripts + Pi Hardware
```

- **Frontend**: React + Vite + TailwindCSS — builds to static files
- **Backend**: Go HTTP server with REST API + WebSocket for live updates
- **Legacy**: Existing bash scripts preserved; Go shells out to them

## Prerequisites

- A Raspberry Pi (Zero W, Zero 2, Pi 4, or Pi 5) or compatible SBC with USB OTG
- A MicroSD card, 64 GB minimum (128 GB+ recommended)
- USB cable to connect the Pi to the Tesla

## Quick Start

**Option A** — Flash the SentryUSB image (recommended):
1. Flash the image to your SD card, boot, open `http://sentryusb.local`
2. Complete the Setup Wizard, wait for reboots (10–20 min), plug into your Tesla

**Option B** — Install on existing Raspberry Pi OS:
```bash
sudo -i
curl -fsSL https://sentryusb.sentry-six.com | bash
```
Then open `http://sentryusb.local` and complete the Setup Wizard.

> **Note:** The Pi will reboot several times during setup — this is normal. Do not power off.

See [Raspberry Pi Setup Guide](doc/RaspberryPiSetup.md) for detailed instructions.

## Development

### Frontend (React)

```bash
cd web
npm install
npm run dev          # Starts Vite dev server on :5173
```

### Backend (Go)

```bash
cd server
go mod tidy
make dev             # Starts Go API server on :8788 in dev mode
```

### Production Build

```bash
cd web && npm run build          # Build frontend
cd ../server && make build-arm64 # Cross-compile for Pi 4/5
cd ../server && make build-armv7 # Cross-compile for Pi Zero
```

## Project Structure

```
Sentry-USB/
├── web/              # React frontend (Vite + TailwindCSS)
│   └── src/
│       ├── components/
│       │   ├── layout/     # AppShell, Sidebar, MobileNav
│       │   └── setup/      # SetupWizard + 9 step components
│       ├── pages/          # Dashboard, Viewer, Files, Logs, Settings
│       └── lib/            # API client, WebSocket, utilities
├── server/           # Go API server
│   ├── api/          # HTTP handlers (status, config, files, system, etc.)
│   ├── config/       # Config file parser/writer
│   ├── shell/        # Safe subprocess execution
│   └── ws/           # WebSocket hub
├── run/              # Runtime scripts (archiveloop, gadget, sync, etc.)
├── setup/            # Pi setup & configuration scripts
└── doc/              # Documentation
```

## Based On

SentryUSB is a modernized fork of [TeslaUSB](https://github.com/marcone/teslausb) by marcone and contributors, fully rebranded and revamped by [Scottmg1](https://github.com/Scottmg1/Sentry-USB).

## Coming from TeslaUSB?

SentryUSB is a full rewrite with a new Go backend and React frontend. **A fresh install is required** — there is no in-place upgrade path from TeslaUSB. Your archive server credentials and notification settings will need to be reconfigured via the web UI Setup Wizard.

## License

MIT — see [LICENSE](LICENSE) for details.
