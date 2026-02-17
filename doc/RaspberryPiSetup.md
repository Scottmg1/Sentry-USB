# SentryUSB — Raspberry Pi Setup Guide

This guide covers installing SentryUSB on a Raspberry Pi. Supported boards:

- **Raspberry Pi 4B / Pi 5** (recommended)
- **Raspberry Pi Zero 2 W** (good budget option)
- **Raspberry Pi Zero W** (works, but slower)

## What You Need

| Part | Details |
|------|---------|
| **Raspberry Pi** | Pi 4B, Pi 5, Pi Zero 2 W, or Pi Zero W |
| **MicroSD card** | 128 GB+ recommended (64 GB minimum) |
| **USB cable** | Pi 4/5: USB-A → USB-C · Pi Zero: USB-A → Micro-USB (use the **data** port, not PWR) |
| **Computer** | With an SD card reader, for flashing |
| **WiFi** | Internet access for initial setup |

> **Pi Zero users**: You must use the port labeled **USB** (not PWR). A micro-USB OTG data cable is required.

There are two ways to install SentryUSB:

- **[Method A: SentryUSB Image](#method-a-sentryusb-image-recommended)** — Flash our pre-built image. Fastest way to get started.
- **[Method B: Manual Install on Raspberry Pi OS](#method-b-manual-install-on-raspberry-pi-os)** — Start from a stock Raspberry Pi OS Bookworm (64-bit Lite) install and add SentryUSB yourself.

---

# Method A: SentryUSB Image (Recommended)

## Quick Overview

```
1. Flash the SentryUSB image to SD card
2. Boot the Pi on your home network
3. Open the web UI → Settings → Setup Wizard
4. Configure everything in the browser
5. Plug into your Tesla
```

## A1. Flash the Image

1. Download the latest [SentryUSB image](https://github.com/Scottmg1/Sentry-USB/releases/latest)
2. Download [Raspberry Pi Imager](https://www.raspberrypi.com/software/)
3. In Pi Imager:
   - **Operating System** → scroll all the way down → **Use custom** → select the downloaded `.img.gz`
   - **Storage** → select your SD card
   - Click the **⚙️ settings gear** and configure:
     - **Hostname**: `sentryusb`
     - **Enable SSH**: Yes, with password authentication
     - **Username**: `pi`
     - **Password**: choose a strong password
     - **WiFi**: Enter your home SSID and password
     - **Locale**: Set your timezone and country
   - Click **Write**

## A2. First Boot

1. Insert the SD card into your Pi
2. **Power only** — use a USB power supply, do NOT plug into the Tesla yet
3. Wait 2–3 minutes for WiFi to connect
4. Verify it's on the network:
   ```bash
   ping sentryusb.local
   ```
   If `.local` doesn't work, check your router's DHCP list for the Pi's IP address.

## A3. Open the Web UI & Configure

1. Open your browser and go to **http://sentryusb.local**
2. Click **Settings** → **Open Wizard**
3. Walk through all 9 steps (see [Setup Wizard Steps](#setup-wizard-steps) below)
4. Click **Apply & Run Setup** on the final step
5. The Pi configures itself and reboots (5–10 minutes)

Then skip ahead to [Plug Into Your Tesla](#plug-into-your-tesla).

---

# Method B: Manual Install on Raspberry Pi OS

Use this if you want to start from a clean **Raspberry Pi OS Bookworm (64-bit Lite)** install. This works on all supported Pi models.

## B1. Flash Raspberry Pi OS

1. Download [Raspberry Pi Imager](https://www.raspberrypi.com/software/)
2. In Pi Imager:
   - **Operating System** → **Raspberry Pi OS (other)** → **Raspberry Pi OS Lite (64-bit)**
     - For Pi Zero W (original): choose **Raspberry Pi OS Lite (32-bit)** instead
   - **Storage** → select your SD card
   - Click the **⚙️ settings gear** and configure:
     - **Hostname**: `sentryusb`
     - **Enable SSH**: Yes, with password authentication
     - **Username**: `pi`
     - **Password**: choose a strong password
     - **WiFi**: Enter your home SSID and password (if you have WiFi — you can also use Ethernet on Pi 4/5)
     - **Locale**: Set your timezone and country
   - Click **Write**

## B2. First Boot & SSH In

1. Insert the SD card into your Pi
2. Power it on with a USB power supply (do NOT plug into the Tesla yet)
3. Wait 2–3 minutes for it to boot and connect to your network
4. SSH in:
   ```bash
   ssh pi@sentryusb.local
   ```
   If `.local` doesn't resolve, check your router for the Pi's IP and use `ssh pi@<ip-address>`.

## B3. Install SentryUSB

Run the one-line installer as root:

```bash
sudo -i
curl -fsSL https://raw.githubusercontent.com/Scottmg1/Sentry-USB/main-dev/install.sh | bash
```

The installer will:
- Detect your Pi's architecture (ARM64 for Pi 4/5/Zero 2, ARMv7 for Pi Zero W)
- Download the correct `sentryusb` binary from [Scottmg1/Sentry-USB](https://github.com/Scottmg1/Sentry-USB/releases)
- Install it as a systemd service on port 80
- Create an initial config file if one doesn't exist
- Set up the USB gadget and partition scripts

Takes about 2–5 minutes depending on your internet speed.

## B4. Open the Web UI & Configure

1. Open your browser and go to **http://sentryusb.local**
2. You should see the SentryUSB dashboard
3. Click **Settings** → **Open Wizard**
4. The wizard will detect your existing WiFi configuration and pre-fill it — you can keep it or change it
5. Walk through all 9 steps (see [Setup Wizard Steps](#setup-wizard-steps) below)
6. Click **Apply & Run Setup** on the final step
7. The Pi configures itself (creating USB drive partitions, setting up archiving, etc.) and reboots. This takes 5–10 minutes. LED flash stages:
   - **2 flashes** → Verifying config
   - **3 flashes** → Downloading scripts
   - **4 flashes** → Creating drive partitions
   - **5 flashes** → Done, rebooting

> If you can't reach the web UI, try `http://<pi-ip-address>` directly, or check the service with `sudo systemctl status sentryusb`.

---

# Setup Wizard Steps

| Step | What You Configure |
|------|-------------------|
| **Welcome** | Overview |
| **Network** | Shows your current WiFi config (if detected) with option to change it, hostname, optional WiFi Access Point for on-the-road access |
| **Storage** | Dashcam size (40G+), optional Music / LightShow / Boombox drives, external NVMe |
| **Archive** | Where to back up clips: SMB/CIFS, rsync, rclone (cloud), NFS, or none |
| **Keep Awake** | Keep car awake during archiving: BLE, TeslaFi, Tessie, or Webhook |
| **Notifications** | Push alerts: Pushover, Discord, Telegram, Slack, Signal, Matrix, AWS SNS, Gotify, IFTTT, Webhook |
| **Security** | Web UI password, SSH public key, disable SSH password auth |
| **Advanced** | Timezone, archive delay, temperature thresholds, CPU governor, update source repo/branch |
| **Review** | Review all settings → **Apply & Run Setup** |

---

# Plug Into Your Tesla

1. Disconnect the Pi from its power supply
2. Connect to your Tesla's USB port:
   - **Pi 4/5**: USB-A to USB-C cable → front console or glovebox USB
   - **Pi Zero**: USB-A to Micro-USB cable → the port labeled **USB** (not PWR)
3. Wait 1–2 minutes. The dashcam icon should appear on the Tesla screen.

> **Important**: Use a **data** cable, not a charge-only cable. If the Tesla doesn't see the drive, try a different cable.

# Accessing the Web UI

| Location | How to Connect |
|----------|---------------|
| **At home** | `http://sentryusb.local` (Pi auto-connects to your WiFi) |
| **On the road** | Connect to the WiFi AP you configured in the wizard, then go to `http://192.168.66.1` |
| **Via USB** | Plug Pi into your computer, SSH to `pi@169.254.x.x` |

# Updating SentryUSB

### From the Web UI (recommended)
1. Go to **Settings**
2. Click **Check for Updates**
3. SentryUSB will check internet, remount the filesystem read-write, download the latest release, and restart automatically

### From SSH
```bash
ssh pi@sentryusb.local
sudo -i
curl -fsSL https://raw.githubusercontent.com/Scottmg1/Sentry-USB/main-dev/install.sh | bash
```

# Troubleshooting

### Pi won't connect to WiFi
- Double-check SSID and password (case-sensitive, watch for special characters)
- Connect a monitor + keyboard to debug
- Pi Zero creates a USB gadget network interface — plug into your computer and try `ssh pi@169.254.x.x`

### Tesla doesn't see the USB drive
- Use a **data** cable, not charge-only
- Pi Zero: make sure you're plugged into the **USB** port, not **PWR**
- Pi 4: the single USB-C port is used for both power and data, so only plug into the Tesla after setup
- Wait 2–3 minutes. Check Dashboard → "USB Drives" should show "Connected"

### Web UI won't load
```bash
ssh pi@sentryusb.local
sudo systemctl status sentryusb
sudo journalctl -u sentryusb -f
```

### Setup fails
- Check **Logs** → "Setup Log" in the web UI
- Common causes: wrong WiFi password, archive server unreachable, SD card too small
- You can re-run the wizard anytime — it's safe to re-apply
