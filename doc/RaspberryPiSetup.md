# SentryUSB — Raspberry Pi Setup Guide

This guide covers installing SentryUSB on a Raspberry Pi. Supported boards:

- **Raspberry Pi 4B / Pi 5** (recommended, 2GB+ RAM)
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

## Quick Overview

```
1. Flash the SentryUSB image to SD card
2. Boot the Pi on your home network
3. SSH in and run the one-line installer
4. Open the web UI → Settings → Setup Wizard
5. Configure everything in the browser
6. Plug into your Tesla
```

---

## Step 1: Flash the Image

1. Download the latest [SentryUSB image](https://github.com/Scottmg1/Sentry-USB/releases/latest) (or the base [TeslaUSB image](https://github.com/marcone/teslausb/releases/latest) if no SentryUSB image is available yet)
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

## Step 2: First Boot

1. Insert the SD card into your Pi
2. **Power only** — use a USB power supply, do NOT plug into the Tesla yet
3. Wait 2–3 minutes for WiFi to connect
4. Verify it's on the network:
   ```bash
   ping sentryusb.local
   ```
   If `.local` doesn't work, check your router's DHCP list for the Pi's IP address.

## Step 3: Install SentryUSB

```bash
ssh pi@sentryusb.local
sudo -i
curl -fsSL https://raw.githubusercontent.com/Scottmg1/Sentry-USB/main-dev/install.sh | bash
```

The installer automatically:
- Detects your Pi model (ARM64 for Pi 4/5/Zero 2, ARMv7 for Pi Zero W)
- Downloads the correct `sentryusb` binary from [Scottmg1/Sentry-USB](https://github.com/Scottmg1/Sentry-USB/releases)
- Installs it as a systemd service on port 80
- Creates an initial config file if one doesn't exist

Takes about 1–2 minutes.

## Step 4: Open the Web UI

1. Open your browser
2. Navigate to **http://sentryusb.local**
3. You should see the SentryUSB dashboard

> If you can't reach it, try `http://<pi-ip-address>` directly.

## Step 5: Run the Setup Wizard

1. Click **Settings** in the sidebar
2. Click **Open Wizard**
3. Walk through all 9 steps:

| Step | What You Configure |
|------|-------------------|
| **Welcome** | Overview |
| **Network** | WiFi SSID/password, hostname, optional WiFi Access Point for on-the-road access |
| **Storage** | Dashcam size (40G+), optional Music / LightShow / Boombox drives, external NVMe |
| **Archive** | Where to back up clips: SMB/CIFS, rsync, rclone (cloud), NFS, or none |
| **Keep Awake** | Keep car awake during archiving: BLE, TeslaFi, Tessie, or Webhook |
| **Notifications** | Push alerts: Pushover, Discord, Telegram, Slack, Signal, Matrix, AWS SNS, Gotify, IFTTT, Webhook |
| **Security** | Web UI password, SSH public key, disable SSH password auth |
| **Advanced** | Timezone, archive delay, temperature thresholds, CPU governor, update source repo/branch |
| **Review** | Review all settings → **Apply & Run Setup** |

4. Click **Apply & Run Setup** on the final step
5. The Pi configures itself and reboots (5–10 minutes). LED flash stages:
   - **2 flashes** → Verifying config
   - **3 flashes** → Downloading scripts
   - **4 flashes** → Creating drive partitions
   - **5 flashes** → Done, rebooting

## Step 6: Plug Into Your Tesla

1. Disconnect the Pi from its power supply
2. Connect to your Tesla's USB port:
   - **Pi 4/5**: USB-A to USB-C cable → front console or glovebox USB
   - **Pi Zero**: USB-A to Micro-USB cable → the port labeled **USB** (not PWR)
3. Wait 1–2 minutes. The dashcam icon should appear on the Tesla screen.

> **Important**: Use a **data** cable, not a charge-only cable. If the Tesla doesn't see the drive, try a different cable.

## Accessing the Web UI

| Location | How to Connect |
|----------|---------------|
| **At home** | `http://sentryusb.local` (Pi auto-connects to your WiFi) |
| **On the road** | Connect to the WiFi AP you configured in the wizard, then go to `http://192.168.66.1` |
| **Via USB** | Plug Pi into your computer, SSH to `pi@169.254.x.x` |

## Updating SentryUSB

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

## Troubleshooting

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
