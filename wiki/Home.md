# SentryUSB Wiki

Welcome to the SentryUSB wiki — your complete reference for setting up, configuring, and maintaining SentryUSB.

## What is SentryUSB?

SentryUSB turns a Raspberry Pi into a smart USB drive for your Tesla's dashcam system. It automatically archives recordings, serves a modern web UI, and can be fully configured through a browser — no SSH or config file editing required.

**Key features:**
- Modern web UI with dark glassmorphism design
- 9-step Setup Wizard — configure everything from your browser
- Multi-camera clip viewer with synchronized playback
- Drive Tracking with GPS-extracted route visualization
- 4 archive methods: CIFS/SMB, rsync, rclone (cloud), NFS
- 11 notification providers
- Single-binary Go backend that cross-compiles for ARM

## Quick Links

| Topic | Description |
|-------|-------------|
| [Getting Started](GettingStarted) | Install, boot, configure — up and running in 20 minutes |
| [Setup Wizard Guide](SetupWizard) | Detailed walkthrough of all 9 wizard steps |
| [Archive Methods](ArchiveMethods) | CIFS/SMB, rsync, rclone, and NFS configuration |
| [Notifications](Notifications) | Configure push notifications (11 providers) |
| [Troubleshooting](Troubleshooting) | Common issues and how to fix them |
| [FAQ](FAQ) | Frequently asked questions |
| [Developer Guide](DeveloperGuide) | Build from source, project structure, contributing |

## Supported Hardware

| Board | Status | Notes |
|-------|--------|-------|
| **Raspberry Pi 4B** | Recommended | Best performance, USB 3.0 |
| **Raspberry Pi 5** | Recommended | Fastest, USB 3.0 |
| **Raspberry Pi Zero 2 W** | Good | Budget option, adequate performance |

**Requirements:**
- MicroSD card: 128 GB+ recommended (64 GB minimum)
- USB data cable (not charge-only) to connect Pi to Tesla
- WiFi network with internet access (for initial setup and updating)

## Links

- **GitHub**: [Scottmg1/Sentry-USB](https://github.com/Scottmg1/Sentry-USB)
- **Releases**: [Latest release](https://github.com/Scottmg1/Sentry-USB/releases/latest)
- **Discord**: [Community chat](https://discord.gg/9QZEzVwdnt)
- **License**: MIT