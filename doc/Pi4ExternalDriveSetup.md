# Using an External USB/NVMe Drive

This guide explains how to use an external USB drive or NVMe SSD with SentryUSB instead of (or in addition to) the SD card for storing dashcam recordings.

## When to Use an External Drive

- You want more storage than your SD card provides
- You prefer the durability and speed of an SSD over an SD card
- You're using a Pi 4 or Pi 5 with available USB ports

## Hardware Requirements

| Part | Details |
|------|---------|
| **Raspberry Pi** | Pi 4B or Pi 5 recommended (USB 3.0 ports) |
| **SD card** | Still required for boot (can be small, e.g., 16 GB) |
| **External drive** | USB SSD, NVMe in a USB enclosure, or USB flash drive |
| **Heatsink/case** | Recommended — Pi 4/5 can get hot under load |

> **Warning**: The selected external drive will be **completely erased** during setup. Back up any data before proceeding.

## Method A: Setup Wizard (Recommended)

1. Connect the external drive to a USB port on your Pi
2. Open **http://sentryusb.local** in your browser
3. Go to **Settings** → **Open Wizard**
4. Navigate to the **Storage** step
5. Under **External Data Drive**, click **Refresh** to detect connected drives
6. Select your drive from the dropdown
7. Configure drive sizes (Dashcam, Music, LightShow, Boombox) as desired
8. Continue through the remaining wizard steps and click **Apply & Run Setup**

SentryUSB will partition the external drive and use it for `/backingfiles` and `/mutable`. The SD card remains read-only and is used only for boot.

## Method B: Manual Configuration (SSH)

1. SSH into the Pi:
   ```bash
   ssh pi@sentryusb.local
   sudo -i
   ```

2. Identify the external drive:
   ```bash
   lsblk
   ```
   Look for the drive (e.g., `/dev/sda`). Make sure you use the **disk** path, not a partition (e.g., `/dev/sda`, not `/dev/sda1`).

3. Edit `/root/sentryusb.conf` and add:
   ```bash
   export DATA_DRIVE=/dev/sda
   ```

4. Run setup to apply:
   ```bash
   /root/bin/setup-sentryusb
   ```

Both `/backingfiles` and `/mutable` will be created on the external drive. The SD card will be read-only.

See the [Raspberry Pi Setup Guide](RaspberryPiSetup.md) for full setup instructions.
