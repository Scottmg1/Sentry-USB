# Archive to Windows File Shares, macOS Sharing, or Samba (CIFS/SMB)

Back up your Tesla dashcam clips to a shared folder on a computer or NAS on your home network using CIFS/SMB.

## Prerequisites

Before configuring Sentry USB, set up a file share on your archive server:

1. **Create a shared folder** on your Windows PC, Mac, or Linux/NAS device (e.g., a share named `SentryArchive`).
2. **Create a user** with read/write access to the share (or use an existing one). Note the username and password.
3. **Verify the server is reachable** from your Pi's WiFi network. You'll need either the server's hostname or IP address.

> **Tip**: To find your server's IP address — on Windows run `ipconfig` in PowerShell; on macOS/Linux run `ifconfig` or `ip addr`.

## Method A: Setup Wizard (Recommended)

The easiest way to configure CIFS/SMB archiving is through the web UI:

1. Open **http://sentryusb.local** in your browser
2. Go to **Settings** → **Open Wizard**
3. Navigate to the **Archive** step
4. Select **CIFS / SMB**
5. Fill in the fields:
   - **Archive Server** — hostname or IP of your file server
   - **Share Name** — name of the shared folder (e.g., `SentryArchive`)
   - **Username** — the share user
   - **Password** — the share password
   - **Domain** — usually not needed; leave empty unless your network requires it
   - **CIFS Version** — usually not needed; leave as default (`3.0`) unless you have an older server
6. Continue through the remaining wizard steps and click **Apply & Run Setup**

## Method B: Manual Configuration (SSH)

For advanced users who prefer to edit the config file directly:

1. SSH into the Pi:
   ```bash
   ssh pi@sentryusb.local
   sudo -i
   ```

2. Verify the archive server is reachable:
   ```bash
   ping -c 3 your-server-hostname
   ```
   If the hostname doesn't resolve, use the IP address instead.

3. Edit `/root/sentryusb.conf` and add/update these variables:
   ```bash
   export ARCHIVE_SYSTEM="cifs"
   export ARCHIVE_SERVER="your-server"
   export SHARE_NAME="SentryArchive"
   export SHARE_USER="username"
   export SHARE_PASSWORD="password"
   ```

4. Run setup to apply:
   ```bash
   /root/bin/setup-sentryusb
   ```

See the [Raspberry Pi Setup Guide](RaspberryPiSetup.md) for full setup instructions.
