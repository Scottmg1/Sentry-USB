# Developer Handoff: SentryUSB Audit & Fixes (Feb 2026)

Summary of changes made to fix critical disk-partitioning/resize failures and WiFi loss after setup, plus UX improvements. Intended for the next developer.

---

## 1. Resizing & Partition Issues

### 1.1 Root partition shrink failing (resize2fs)

**Problem:** After install, the initramfs shrink step could fail silently. Users saw "Previous resize attempt failed" and multiple reboots. Root had ~4G used but the script targeted a hardcoded **3G** size, so `resize2fs` could not shrink below used space.

**Fix:**
- **`install.sh`** and **`setup/generic/install.sh`**: Replaced hardcoded `3G` with a **dynamic target**: `(current used space + 2GB headroom)`, minimum **6GB**. Uses `df --output=used -k /` to compute.
- **`tools/debian-resizefs.sh`**: Initramfs script now writes a **`/root/RESIZE_RESULT`** marker with `success`, `fail:e2fsck:RC`, or `fail:resize2fs:RC`. Copy `mount`/`umount` into initramfs so the marker can be written to the root fs.
- **`install.sh`** / **`setup/generic/install.sh`**: On boot after initramfs resize, **read RESIZE_RESULT** and either clear the marker, or `error_exit` with a clear message (e.g. suggest running `resize2fs` or `e2fsck` manually from recovery).

**Files:** `install.sh`, `setup/generic/install.sh`, `tools/debian-resizefs.sh`

### 1.2 Partition number extraction (multi-digit)

**Problem:** Scripts used Bash substring `${var:0-1}` or `${var:0:-1}` for partition numbers. That only works for single digits (e.g. `mmcblk0p2` → `2`). For devices with partition 10+ it breaks.

**Fix:** Use:
```bash
partnum=$(echo "$rootpart" | grep -o '[0-9]*$')
```
For "prefix" (device without partition number):
```bash
prefix=$(echo "$DEVICE" | sed 's/[0-9]*$//')
```
**Files:** `install.sh`, `setup/generic/install.sh`, `setup/pi/setup-sentryusb`, `setup/pi/create-backingfiles-partition.sh`

### 1.3 ShellCheck SC2155

**Problem:** `readonly VAR=$(...)` masks the exit code of the command.

**Fix:** Assign then declare:
```bash
VAR=$(...)
readonly VAR
```
**Files:** `setup/pi/setup-sentryusb`, `setup/pi/create-backingfiles-partition.sh`

### 1.4 Go build / server static directory

**Problem:** `make build` failed with `pattern all:static: no matching files found`. On fresh clone, `server/static/` did not exist; `copy-static` tried to copy into it and the Go `//go:embed all:static` directive failed. Install script still reported "Binary built and installed" on build failure.

**Fix:**
- **`server/Makefile`**: In `copy-static` target, run **`mkdir -p static`** before `rm -rf static/*` and copy.
- **`server/static/.gitkeep`**: Added so `static/` is tracked in git and exists on clone.
- **`install.sh`**: Before `make build`, **`mkdir -p static`**. Check **exit code** of `make build`; only report success and copy the binary if build succeeded; otherwise warn and do not copy.

**Files:** `server/Makefile`, `server/static/.gitkeep`, `install.sh`

### 1.5 Backing file images (FAT32 resize vs recreate)

**Problem:** Changing CAM/Music/etc. sizes in the wizard always recreated images (destructive). No in-place resize when possible.

**Fix:**
- **`tools/resize-image.sh`**: Detect filesystem with `blkid`; **explicitly refuse exFAT** (fatresize doesn’t support it). For FAT32: `udevadm settle` after losetup; on grow, if `fatresize` fails after `fallocate`, **truncate back** to original size and exit 1. Only install `fatresize` when actually needed (FAT32).
- **`setup/pi/create-backingfiles.sh`**: New **`try_resize_image()`**; **`create_drive()`** (destructive) separated from **`add_drive()`**. `add_drive()` tries **`try_resize_image()`** first; only calls `create_drive()` if resize not possible or fails. Loop over images to see if any need recreate; only then prompt "Some drives must be recreated (resize was not possible)."
- **`setup/pi/setup-sentryusb`**: **`copy_script tools/resize-image.sh /root/bin`** before calling create-backingfiles so resize script is present.

**Files:** `tools/resize-image.sh`, `setup/pi/create-backingfiles.sh`, `setup/pi/setup-sentryusb`

### 1.6 udev settle after partition/loop changes

**Problem:** Race conditions when creating partitions or loop devices; scripts sometimes proceeded before the kernel had the new device nodes.

**Fix:** After `parted`, `partprobe`, `partx`, or `losetup --show`, add:
```bash
udevadm settle --timeout=10 2>/dev/null || sleep 2
```
**Files:** `setup/pi/create-backingfiles-partition.sh`, `setup/pi/create-backingfiles.sh`, `tools/resize-image.sh`

---

## 2. WiFi / AP Loss After Setup (Read-Only Root)

### 2.1 Root cause

**Problem:** After the setup wizard finished and the Pi rebooted, WiFi (and sometimes Ethernet) stopped working. Kernel logs showed:
- `dnsmasq: cannot open or create lease file /var/lib/NetworkManager/dnsmasq-ap0.leases: Read-only file system`
- `brcmf_cfg80211_stop_ap: setting AP mode failed -52` in a loop

**Cause:** **`make-root-fs-readonly.sh`** had moved networking state to **`/mutable`** via symlinks:
- `/var/lib/NetworkManager` → `/mutable/var/lib/NetworkManager`
- `/etc/NetworkManager/system-connections` → `/mutable/etc/...`
- `/var/lib/dhcp`, `/var/lib/dhcpcd` → `/mutable/...`
- `resolv.conf` → `/mutable/resolv.conf`

When **`/mutable` lives on a USB drive** (`DATA_DRIVE`), it may not be mounted (or writable) when NetworkManager and dnsmasq start. The AP connection uses `ipv4.method shared`, so dnsmasq must write lease files under `/var/lib/NetworkManager`. Write fails → dnsmasq exits → NM disables AP → NM retries → infinite loop, thrashing the single WiFi radio and breaking both AP and client WiFi.

### 2.2 Fix (new installs)

**`setup/pi/make-root-fs-readonly.sh`** was changed so networking does **not** depend on `/mutable`:

| What | Before | After |
|------|--------|--------|
| `/var/lib/NetworkManager` | Symlink to `/mutable` | **tmpfs** mount (fstab) |
| NM connection profiles | Moved to `/mutable` | **Kept on root**; backup copied to `/mutable` |
| `/var/lib/dhcp`, `/var/lib/dhcpcd` | Symlinks to `/mutable` | **tmpfs** mounts |
| `resolv.conf` | Symlink to `/mutable` | Symlink to **`/tmp/resolv.conf`** |

- Undo logic for **existing symlinks** (upgrade path): remove symlink, create real dir or restore from `/mutable` backup.
- **fstab**: Add tmpfs lines for `/var/lib/NetworkManager`, `/var/lib/dhcp`, `/var/lib/dhcpcd` (idempotent).

**`setup/pi/create-backingfiles-partition.sh`**  
- Add **`nofail`** to **`LABEL=mutable`** and **`LABEL=backingfiles`** in fstab (new entries and existing ones via sed) so the system doesn’t hang in emergency mode if the USB drive is missing or slow.

**Files:** `setup/pi/make-root-fs-readonly.sh`, `setup/pi/create-backingfiles-partition.sh`

### 2.3 Fix for already-installed systems

Users who already completed setup with the old script need a one-time fix without re-running the full wizard.

- **New script:** **`setup/pi/fix-readonly-networking.sh`**
  - **Early check:** Before changing anything, the script checks whether the old broken state is present (any of: `/var/lib/NetworkManager` or `system-connections` or dhcp/dhcpcd are symlinks; `resolv.conf` points to `/mutable`; tmpfs entries missing in fstab; mutable/backingfiles in fstab without `nofail`). If **none** of these are true, it prints *"No fix needed: networking is already using tmpfs / root (not symlinks to /mutable)."* and **exits without making changes or asking for reboot**.
  - If fix is needed: replaces the symlinks with real dirs, restores NM profiles from `/mutable` if present, points `resolv.conf` at `/tmp`, adds tmpfs and `nofail` fstab entries as needed.
  - Safe to run multiple times. Requires root; run after `remountfs_rw` if root is read-only.

- **New command in setup-sentryusb:** **`fix_networking`**
  - Requires `SENTRYUSB_SETUP_FINISHED`.
  - Calls `/root/bin/remountfs_rw`, then runs `fix-readonly-networking.sh`.
  - Tells user to reboot.

**Usage for existing users (SSH or serial):**
```bash
sudo -i
/root/bin/setup-sentryusb fix_networking
reboot
```
(They need the latest scripts, e.g. via upgrade or re-download.)

**Files:** `setup/pi/fix-readonly-networking.sh`, `setup/pi/setup-sentryusb`, `README.md` (section "Fix WiFi/AP after setup (existing installs)")

---

## 3. Log Viewer UX (Web UI)

**Problem:** The Archive Loop (and other) log view scrolled to the bottom on every 2s poll, so users couldn’t scroll up to read older lines.

**Fix:** **`web/src/pages/Logs.tsx`**
- **Follow mode:** Auto-scroll to bottom only when the user is already near the bottom (within ~60px). Use a **ref** (`followRef`) so changing “at bottom” doesn’t restart the poll interval.
- When the user scrolls up, auto-scroll stops.
- **“Follow” button** appears when not at bottom; clicking it scrolls to bottom and re-enables follow mode.
- First load still scrolls to bottom.

**File:** `web/src/pages/Logs.tsx`

---

## 4. File Checklist (Quick Reference)

| Area | Files touched |
|------|----------------|
| Root resize / initramfs | `install.sh`, `setup/generic/install.sh`, `tools/debian-resizefs.sh` |
| Partition number / SC2155 | `install.sh`, `setup/generic/install.sh`, `setup/pi/setup-sentryusb`, `setup/pi/create-backingfiles-partition.sh` |
| Go build / static | `server/Makefile`, `server/static/.gitkeep`, `install.sh` |
| Image resize (FAT32) | `tools/resize-image.sh`, `setup/pi/create-backingfiles.sh`, `setup/pi/setup-sentryusb` |
| udev settle | `setup/pi/create-backingfiles-partition.sh`, `setup/pi/create-backingfiles.sh`, `tools/resize-image.sh` |
| Read-only WiFi fix | `setup/pi/make-root-fs-readonly.sh`, `setup/pi/create-backingfiles-partition.sh` |
| Fix for existing installs | `setup/pi/fix-readonly-networking.sh`, `setup/pi/setup-sentryusb`, `README.md` |
| Logs page | `web/src/pages/Logs.tsx` |

---

## 5. Testing Suggestions

- **Resize:** On a Pi with root near full, run install and confirm root shrink uses a dynamic target and that RESIZE_RESULT is read after reboot; test both success and failure paths if possible.
- **WiFi:** New install with USB data drive: complete wizard, reboot, confirm WiFi and AP work. Existing install: run `setup-sentryusb fix_networking`, reboot, confirm WiFi/AP.
- **Build:** Fresh clone, `cd server`, `make build` (with and without `web/dist`); confirm no embed error and that install.sh only reports success when build actually succeeds.
- **Logs page:** Open Logs, scroll up, confirm view doesn’t jump to bottom on refresh; click Follow and confirm it scrolls back to bottom.

---

*Document generated from audit and fixes completed Feb 2026.*
