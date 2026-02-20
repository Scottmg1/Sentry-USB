# Archive with rsync (SSH-based File Sync)

Back up your Tesla dashcam clips to a remote server using [rsync](https://rsync.samba.org/) over SSH.

## Prerequisites

- A server (Linux box, NAS, another Raspberry Pi, etc.) with SSH and rsync installed
- The server's hostname or IP address, an SSH username, and a destination path
- The server must allow SSH key-based authentication (you'll set up a keypair)

## Method A: Setup Wizard + SSH Key Setup (Recommended)

The wizard configures the rsync settings, but you still need to set up SSH key authentication manually.

### Step 1: Configure rsync in the wizard

1. Open **http://sentryusb.local** in your browser
2. Go to **Settings** → **Open Wizard**
3. Navigate to the **Archive** step
4. Select **rsync**
5. Fill in the fields:
   - **Server** — hostname or IP of your archive server
   - **Username** — the SSH user on the server
   - **Remote Path** — destination directory (e.g., `/mnt/storage/SentryArchive/`)
6. Continue through the remaining wizard steps and click **Apply & Run Setup**

### Step 2: Set up SSH key authentication

After the wizard completes and the Pi is set up, SSH in and create a keypair so rsync can authenticate without a password:

```bash
ssh pi@sentryusb.local
sudo -i
/root/bin/remountfs_rw
ssh-keygen          # Press Enter for all defaults (no passphrase)
ssh-copy-id user@archiveserver
```

Replace `user@archiveserver` with your actual username and server. This copies the public key to the server and adds it to `known_hosts`.

> **Tip**: Additional SSH options (e.g., custom port) can be configured in `~/.ssh/config`. See the [ssh_config man page](https://linux.die.net/man/5/ssh_config).

## Method B: Manual Configuration (SSH)

For advanced users who prefer to edit the config file directly:

1. SSH into the Pi and set up SSH keys (see Step 2 above)

2. Edit `/root/sentryusb.conf` and add/update these variables:
   ```bash
   export ARCHIVE_SYSTEM=rsync
   export RSYNC_USER=pi
   export RSYNC_SERVER=192.168.1.254
   export RSYNC_PATH=/mnt/storage/SentryArchive/
   ```

   | Variable | Description |
   |----------|-------------|
   | `ARCHIVE_SYSTEM` | Must be `rsync` |
   | `RSYNC_USER` | SSH username on the archive server |
   | `RSYNC_SERVER` | Hostname or IP address of the server |
   | `RSYNC_PATH` | Destination directory on the server |

3. Run setup to apply:
   ```bash
   /root/bin/setup-sentryusb
   ```

See the [Raspberry Pi Setup Guide](RaspberryPiSetup.md) for full setup instructions.
