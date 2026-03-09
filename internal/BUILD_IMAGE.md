# Building SentryUSB Images Locally

This guide explains how to build custom SentryUSB Raspberry Pi images on your Windows desktop using WSL2 and Docker.

## Prerequisites

### Required Software
- **Windows 10/11** with WSL2 enabled
- **WSL2 Ubuntu distribution** (tested with Ubuntu 24.04)
- **Docker** installed in WSL2
- **Go 1.23+** installed on Windows
- **Node.js 20+** installed on Windows

### One-Time WSL2 Setup

1. **Install WSL2** (if not already installed):
   ```powershell
   wsl --install -d Ubuntu
   ```

2. **Install Docker in WSL2**:
   ```bash
   wsl -d Ubuntu
   sudo apt-get update
   sudo apt-get install -y docker.io qemu-user-static binfmt-support
   sudo usermod -aG docker $USER
   ```

3. **Start Docker service** (required each time you restart WSL):
   ```bash
   sudo service docker start
   ```

## Build Process

### Step 1: Build Go Binaries (Windows)

The SentryUSB server embeds the web frontend, so we build the frontend first, then cross-compile the Go binaries.

```powershell
# Navigate to the web directory
cd c:\Users\scott\Documents\Sentry-Six-Assets\SentryUSB\web

# Install dependencies and build frontend
npm ci --no-audit --no-fund
npm run build

# Navigate to server directory
cd ..\server

# Copy built frontend to server/static
if (Test-Path "static") { Remove-Item -Recurse -Force "static" }
Copy-Item -Recurse "..\web\dist" "static"

# Build ARM64 binary
$env:GOOS="linux"; $env:GOARCH="arm64"; go build -o bin/sentryusb-linux-arm64 .

# Build ARMv7 (32-bit) binary
$env:GOOS="linux"; $env:GOARCH="arm"; $env:GOARM="7"; go build -o bin/sentryusb-linux-armv7 .
```

**Result**: You now have:
- `server/bin/sentryusb-linux-arm64` (for Raspberry Pi 3/4/5, Pi Zero 2 W in 64-bit mode)
- `server/bin/sentryusb-linux-armv7` (for Raspberry Pi 3/4/5 in 32-bit mode, older models)

### Step 2: Build Raspberry Pi Image (WSL2)

The `build-local.sh` script handles the entire pi-gen build process.

```bash
# Run from WSL2
wsl -d Ubuntu

# Start Docker if needed
sudo service docker start

# Run the build script (arm64 or armhf)
sudo bash /mnt/c/Users/scott/Documents/Sentry-Six-Assets/SentryUSB/build-local.sh arm64
```

**Build time**: ~35-40 minutes on typical hardware (uses QEMU emulation for ARM)

**What the script does**:
1. Clones the appropriate `pi-gen` branch (arm64 or master for 32-bit)
2. Copies your config and binaries from Windows to native Linux filesystem
3. Fixes any Windows line endings (CRLF → LF)
4. Increases image partition size margin for Debian Trixie's larger apt indices
5. Runs `pi-gen` inside Docker to build the image
6. Outputs compressed image to `/tmp/pi-gen/deploy/`

### Step 3: Copy Image to Windows

After the build completes, copy the image back to your Windows filesystem:

```bash
# Still in WSL2
sudo cp /tmp/pi-gen/deploy/image_*.zip /mnt/c/Users/scott/Documents/Sentry-Six-Assets/SentryUSB/deploy/
```

The image will be at: `c:\Users\scott\Documents\Sentry-Six-Assets\SentryUSB\deploy\image_YYYY-MM-DD-sentryusb.zip`

## Architecture Options

### ARM64 (64-bit) - Recommended
```bash
sudo bash /mnt/c/Users/scott/Documents/Sentry-Six-Assets/SentryUSB/build-local.sh arm64
```
- **Compatible with**: Raspberry Pi 3, 4, 5, Zero 2 W, Compute Module 3/4
- **Based on**: pi-gen `arm64` branch + Debian Trixie (13)
- **Image size**: ~791MB compressed

### ARMv7 (32-bit) - Legacy
```bash
sudo bash /mnt/c/Users/scott/Documents/Sentry-Six-Assets/SentryUSB/build-local.sh armhf
```
- **Compatible with**: All Raspberry Pi models (including Pi 1, Zero)
- **Based on**: pi-gen `master` branch + Debian Trixie (13)
- **Note**: Slower than 64-bit on capable hardware

## Configuration

Key configuration files are in `pi-gen-sources/`:

- **`pi-gen-config`** (ARM64 settings):
  - Default user: `sentryusb` / `raspberry`
  - Hostname: `sentryusb`
  - Debian release: `trixie`
  - Stages: `stage0`, `stage1`, `stage2`, `stage_sentryusb`

- **`pi-gen-config-32bit`** (ARMv7 settings):
  - Same as ARM64 but for 32-bit builds

- **`00-sentryusb-tweaks/`** (custom stage):
  - `00-packages`: Packages to install
  - `00-run.sh`: Setup script (runs in chroot)
  - `files/`: Files copied to image (rc.local, systemd services, etc.)

## Flashing & First Boot

1. **Extract the zip** to get the `.img` file
2. **Flash to SD card** using [Raspberry Pi Imager](https://www.raspberrypi.com/software/)
   - Click "Choose OS" → "Use custom" → select your `.img`
   - Click the gear icon to configure WiFi/SSH (recommended)
3. **Boot the Pi** and wait ~30 seconds
4. **Navigate to** `http://sentryusb.local` in your browser
5. **Complete the setup wizard** (network, storage, notifications, etc.)

### Default Credentials
- **SSH**: `sentryusb` / `raspberry` (can be changed in Raspberry Pi Imager)
- **Web UI**: No password on first boot (set in wizard)

### Setup Flow
- **First boot** → Web UI shows Setup Wizard
- **Wizard completed** → Downloads full setup script from GitHub, reboots
- **Setup running** → Progress screen (may take 10-20 minutes)
- **Setup complete** → Dashboard available

## Troubleshooting

### Docker Permission Denied
```bash
# Add your user to docker group
sudo usermod -aG docker $USER
# Log out and back in, or:
newgrp docker
```

### Build Container Already Exists
```bash
# Remove old container
sudo docker rm -v pigen_work
```

### Out of Space During Build
The image partition size is auto-calculated. If builds fail with disk space errors:
1. Check WSL2 has enough space: `df -h /`
2. The margin is already increased to 800MB in `build-local.sh`

### QEMU Errors
Ensure QEMU is installed in WSL2:
```bash
sudo apt-get install -y qemu-user-static binfmt-support
```

### Build Logs
Check `/tmp/pi-gen/deploy/build-docker.log` and `build.log` for errors.

### Windows Line Endings
The build script automatically fixes CRLF issues. If you edit scripts directly in WSL, ensure they use LF line endings:
```bash
dos2unix /path/to/script.sh
```

## Advanced Customization

### Add Packages
Edit `pi-gen-sources/00-sentryusb-tweaks/00-packages` and add package names (one per line).

### Modify First Boot
Edit `pi-gen-sources/00-sentryusb-tweaks/files/rc.local` to customize what happens on first boot.

### Change Hostname/User
Edit `pi-gen-sources/pi-gen-config` (or `pi-gen-config-32bit`):
```
TARGET_HOSTNAME=your-hostname
FIRST_USER_NAME=your-username
FIRST_USER_PASS=your-password
```

### Clean Build Environment
```bash
# Remove all build artifacts
sudo rm -rf /tmp/pi-gen /tmp/sentryusb-src
sudo docker rm -v pigen_work
```

## Known Issues

- **Build time**: ARM64 builds take 35-40 minutes due to QEMU emulation on x86_64 hosts
- **Native ARM runner**: If you have access to an ARM64 system, build time drops to ~15-20 minutes
- **Memory usage**: Docker may require 4GB+ RAM during peak build stages
- **WSL2 disk**: Ensure at least 20GB free space in your WSL2 virtual disk

## File Locations Summary

### Windows (Development)
- Binaries: `server/bin/sentryusb-linux-*`
- Frontend: `web/dist/` → `server/static/`
- Output images: `deploy/image_*.zip`

### WSL2 (Build)
- pi-gen clone: `/tmp/pi-gen/`
- Source copy: `/tmp/sentryusb-src/`
- Build output: `/tmp/pi-gen/deploy/`

### Image (Raspberry Pi)
- SentryUSB binary: `/usr/local/bin/sentryusb-binary`
- Boot partition: `/boot/firmware/` (symlinked to `/sentryusb`)
- Config: `/root/sentryusb.conf` (after setup wizard)
- Setup markers: `/sentryusb/SENTRYUSB_SETUP_*`
- Logs: `/sentryusb/sentryusb-setup.log`
