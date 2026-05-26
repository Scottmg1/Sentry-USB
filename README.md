# Sentry USB

> [!IMPORTANT]
> **This Go version has been replaced by [Sentry USB Rusty](https://github.com/Sentry-Six/Sentry-USB-Rusty)** — a Rust rewrite that's faster, lighter, and more reliable.
>
> - **Existing installs keep working and still receive updates** through the in-app updater.
> - **New installs should use Rusty** — see the [Rusty installer](https://github.com/Sentry-Six/Sentry-USB-Rusty#install-fresh-pi).
> - **Migrating from Go to Rusty is a one-time reflash.** Back up your `sentryusb.conf` first; the file is drop-in compatible.

A modern, feature-rich USB drive manager for Tesla vehicles — built on the foundation of [TeslaUSB](https://github.com/marcone/teslausb), fully rebranded and revamped at [Scottmg1/Sentry-USB](https://github.com/Scottmg1/Sentry-USB).

## What is Sentry USB?

Sentry USB turns a Raspberry Pi (or compatible SBC) into a smart USB drive for your Tesla's dashcam system. It automatically archives recordings, serves a modern web UI, and can be fully configured through a browser — no SSH or config file editing required.

**Key features:**
- **Modern Web UI** — Dark glassmorphism design with sidebar navigation, real-time dashboard, multi-camera viewer, file browser, and live log tailing
- **Setup Wizard** — 9-step guided configuration covering WiFi, storage, archiving, keep-awake, notifications, security, and advanced settings — all from your browser
- **Go API Server** — Lightweight single-binary backend that cross-compiles for ARM, replacing nginx + CGI scripts
- **Multi-camera Viewer** — 6 layout options for synchronized playback of all Tesla camera angles
- **Archive Support** — CIFS/SMB, rsync, rclone (cloud), and NFS
- **Keep-Awake** — BLE, TeslaFi, Tessie, and Webhook methods
- **13+ Notification Providers** — Pushover, Discord, Telegram, Slack, Signal, Matrix, AWS SNS, IFTTT, Gotify, NTFY, Webhooks, Sentry Connect

## Privacy

By default, Sentry USB does **not** transmit any device identifier to our
servers. Everything it sends out, listed in one place:

| When | What | Identifier? |
|---|---|---|
| Daily update check | Software version, CPU arch, board model | None by default |
| Once per install | Empty ping (no body) | None — anonymous counter |
| Wraps / lock chime submissions | The file + your IP for rate-limiting | None (no device fingerprint) |
| iOS push pairing (if enabled) | Random pairing ID | Not tied to hardware |

The only way a device fingerprint is sent is if you explicitly opt in to
**Settings → Privacy → Help us count new installs** (default: off). That's
the GDPR Art. 21 right-to-object mechanism — it's a toggle, not an email,
and the default is the opted-out state. Full details in
[`wiki/Privacy.md`](wiki/Privacy.md).

## Documentation

- **[Wiki](https://github.com/Scottmg1/Sentry-USB/wiki)** — Getting Started, Setup Wizard Guide, Archive Methods, Notifications, Privacy, Troubleshooting, FAQ, Developer Guide
- **[Build Guide](BUILD.md)** — Building from source

## Prerequisites

- A Raspberry Pi (Zero 2 W, Pi 3, Pi 4, or Pi 5) or compatible SBC with USB OTG
- A MicroSD card, 64 GB minimum (128 GB+ recommended)
- USB cable to connect the Pi to the Tesla

## Quick Start

1. Flash **Raspberry Pi OS Lite (64-bit)** to your SD card using **Raspberry Pi Imager** — configure WiFi, hostname (`sentryusb`), and SSH in the imager settings before writing
2. Boot the Pi, SSH in, and install:
```bash
sudo -i
curl -fsSL https://usb.sentry-six.com | bash
```
3. Open `http://sentryusb.local`, complete the Setup Wizard, wait for reboots (10–20 min), plug into your Tesla

> **Note:** The Pi will reboot several times during setup — this is normal. Do not power off.

See the [Getting Started guide](https://github.com/Scottmg1/Sentry-USB/wiki/GettingStarted) for detailed instructions.

## Based On

Sentry USB is a modernized fork of [TeslaUSB](https://github.com/marcone/teslausb) by marcone and contributors, fully rebranded and revamped by [Scottmg1](https://github.com/Scottmg1/Sentry-USB).

## Coming from TeslaUSB?

Sentry USB is a full rewrite with a new Go backend and React frontend. **A fresh install is required** — there is no in-place upgrade path from TeslaUSB. Your archive server credentials and notification settings will need to be reconfigured via the web UI Setup Wizard.

## License

MIT — see [LICENSE](LICENSE) for details.
