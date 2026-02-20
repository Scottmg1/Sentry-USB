# Archive with rclone (Cloud Storage)

Back up your Tesla dashcam clips to cloud storage (Google Drive, S3, Dropbox, OneDrive, etc.) using [rclone](https://rclone.org/).

## Prerequisites

- An account on a supported cloud storage service (see the [rclone provider list](https://rclone.org/#providers))
- SSH access to your Pi (rclone requires an interactive terminal setup)

> **Note**: Unlike other archive methods, rclone requires an SSH session to run `rclone config` interactively on the Pi. The Setup Wizard configures the remote name and path, but the rclone remote itself must be created via SSH.

## Method A: Setup Wizard + SSH rclone Config (Recommended)

### Step 1: Set up SentryUSB with rclone selected

1. Open **http://sentryusb.local** in your browser
2. Go to **Settings** → **Open Wizard**
3. Navigate to the **Archive** step
4. Select **rclone**
5. Fill in the fields:
   - **Remote Name** — the name you'll use when creating the rclone remote (e.g., `gdrive`)
   - **Remote Path** — the folder on the remote to store clips (e.g., `SentryArchive`)
   - **Archive Server** — an IP for connectivity checks (e.g., `8.8.8.8`)
6. Continue through the remaining wizard steps and click **Apply & Run Setup**

### Step 2: Install and configure rclone via SSH

After setup completes and the Pi reboots:

```bash
ssh pi@sentryusb.local
sudo -i
/root/bin/remountfs_rw

# Install rclone
curl https://rclone.org/install.sh | sudo bash

# Configure a remote — use the same name you entered in the wizard (e.g., gdrive)
rclone config
```

Follow the interactive prompts for your chosen cloud provider. See [rclone.org](https://rclone.org/) for provider-specific instructions.

- For **Google Drive**: use the `drive.file` scope ([details](https://rclone.org/drive/#scopes))
- Name the remote exactly what you entered in the wizard's **Remote Name** field

### Step 3: Verify the remote

```bash
# Confirm the remote exists
rclone listremotes

# Create the archive folder
rclone mkdir "gdrive:SentryArchive"

# Verify it was created
rclone lsd "gdrive":
```

Replace `gdrive` and `SentryArchive` with your actual remote name and path.

### Step 4: Apply

```bash
/root/bin/setup-sentryusb
```

## Method B: Full Manual Configuration (SSH)

For advanced users who prefer to do everything via SSH:

1. SSH into the Pi, install rclone, and run `rclone config` (see Step 2 above)

2. Edit `/root/sentryusb.conf` and add/update these variables:
   ```bash
   export ARCHIVE_SYSTEM=rclone
   export RCLONE_DRIVE="gdrive"
   export RCLONE_PATH="SentryArchive"
   export RCLONE_FLAGS=()
   ```

   | Variable | Description |
   |----------|-------------|
   | `ARCHIVE_SYSTEM` | Must be `rclone` |
   | `RCLONE_DRIVE` | Remote name (shown by `rclone listremotes`) |
   | `RCLONE_PATH` | Folder on the remote for archived clips |
   | `RCLONE_FLAGS` | Optional flags, e.g., `(--checksum)` or `(--flag1 --flag2)` |

3. Verify: `rclone ls "$RCLONE_DRIVE:$RCLONE_PATH"` should not error (may print nothing if empty).

4. Run setup to apply:
   ```bash
   /root/bin/setup-sentryusb
   ```

See the [Raspberry Pi Setup Guide](RaspberryPiSetup.md) for full setup instructions.
