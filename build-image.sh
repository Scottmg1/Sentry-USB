#!/bin/bash -eu
#
# Build a SentryUSB Raspberry Pi image locally using pi-gen + Docker.
#
# Prerequisites:
#   - Docker installed and running
#   - Internet access (to download Raspberry Pi OS base)
#
# Usage:
#   ./build-image.sh                         # 64-bit image (Pi 3/4/5/Zero 2)
#   ./build-image.sh --32bit                 # 32-bit image (Pi Zero W)
#   ./build-image.sh /path/to/binary         # 64-bit with local binary
#   ./build-image.sh --32bit /path/to/binary # 32-bit with local binary
#
# Output:
#   deploy/sentryusb-*.img.gz — ready to flash with Raspberry Pi Imager
#

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
WORK_DIR="/tmp/sentryusb/pi-gen"
REPO="Scottmg1/Sentry-USB"

RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m'

info()  { echo -e "${BLUE}[INFO]${NC} $1"; }
ok()    { echo -e "${GREEN}[OK]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

# ── Parse arguments ──
BUILD_32BIT=false
LOCAL_BINARY=""
for arg in "$@"; do
    case "$arg" in
        --32bit|--32|--armhf|--pizero)
            BUILD_32BIT=true
            ;;
        *)
            if [ -f "$arg" ]; then
                LOCAL_BINARY="$(cd "$(dirname "$arg")" && pwd)/$(basename "$arg")"
            fi
            ;;
    esac
done

if $BUILD_32BIT; then
    ARCH_LABEL="32-bit (armhf — Pi Zero W)"
    BINARY_SUFFIX="linux-armv6"
    GO_ARCH="arm"
    GO_ARM="6"
    CONFIG_FILE="pi-gen-config-32bit"
else
    ARCH_LABEL="64-bit (arm64 — Pi 3/4/5/Zero 2)"
    BINARY_SUFFIX="linux-arm64"
    GO_ARCH="arm64"
    GO_ARM=""
    CONFIG_FILE="pi-gen-config"
fi

info "Building $ARCH_LABEL image"

# Check prerequisites
command -v docker &>/dev/null || error "Docker is required. Install it first."

# ── Step 1: Get the SentryUSB binary ──
BINARY_PATH=""
if [ -n "$LOCAL_BINARY" ]; then
    BINARY_PATH="$LOCAL_BINARY"
    info "Using local binary: $BINARY_PATH"
else
    info "Building binary from source..."
    if command -v go &>/dev/null && command -v node &>/dev/null; then
        (
            cd "$SCRIPT_DIR/web"
            npm ci --no-audit --no-fund 2>&1 | tail -3
            npm run build 2>&1 | tail -3
        )
        (
            cd "$SCRIPT_DIR/server"
            make copy-static
            if [ -n "$GO_ARM" ]; then
                GOOS=linux GOARCH=$GO_ARCH GOARM=$GO_ARM go build -o bin/sentryusb-$BINARY_SUFFIX .
            else
                GOOS=linux GOARCH=$GO_ARCH go build -o bin/sentryusb-$BINARY_SUFFIX .
            fi
        )
        BINARY_PATH="$SCRIPT_DIR/server/bin/sentryusb-$BINARY_SUFFIX"
        ok "Binary built: $BINARY_PATH"
    else
        info "Go/Node not available locally. Downloading from GitHub releases..."
        BINARY_PATH="/tmp/sentryusb-$BINARY_SUFFIX"
        curl -fsSL "https://github.com/$REPO/releases/latest/download/sentryusb-$BINARY_SUFFIX" -o "$BINARY_PATH" \
            || error "Failed to download binary. Build it locally first:\n  cd web && npm ci && npm run build\n  cd ../server && make copy-static build-arm64"
        ok "Binary downloaded"
    fi
fi

[ -f "$BINARY_PATH" ] || error "Binary not found at $BINARY_PATH"

# ── Step 1b: Build tesla-control and tesla-keygen ──
# tesla-control is the Tesla BLE command-line tool used by awake_start for
# Keep Awake BLE mode. Tesla does not publish pre-built binaries, so we build
# from source. Requires Go 1.23+.
TESLA_CONTROL_PATH=""
TESLA_KEYGEN_PATH=""
if command -v go &>/dev/null; then
    info "Building tesla-control and tesla-keygen from source..."
    TESLA_VC_DIR="/tmp/sentryusb-vehicle-command"
    rm -rf "$TESLA_VC_DIR"
    git clone --depth 1 https://github.com/teslamotors/vehicle-command.git "$TESLA_VC_DIR"
    (
        cd "$TESLA_VC_DIR"
        if [ -n "$GO_ARM" ]; then
            GOOS=linux GOARCH=$GO_ARCH GOARM=$GO_ARM go build -o tesla-control ./cmd/tesla-control
            GOOS=linux GOARCH=$GO_ARCH GOARM=$GO_ARM go build -o tesla-keygen ./cmd/tesla-keygen
        else
            GOOS=linux GOARCH=$GO_ARCH go build -o tesla-control ./cmd/tesla-control
            GOOS=linux GOARCH=$GO_ARCH go build -o tesla-keygen ./cmd/tesla-keygen
        fi
    )
    TESLA_CONTROL_PATH="$TESLA_VC_DIR/tesla-control"
    TESLA_KEYGEN_PATH="$TESLA_VC_DIR/tesla-keygen"
    ok "tesla-control and tesla-keygen built"
else
    info "Go not available — tesla-control will not be bundled (Keep Awake BLE requires it)"
fi

# ── Step 2: Clone pi-gen ──
info "Setting up pi-gen..."
rm -rf "$WORK_DIR"
if $BUILD_32BIT; then
    git clone --depth 1 https://github.com/RPi-Distro/pi-gen.git "$WORK_DIR"
else
    git clone --depth 1 --branch arm64 https://github.com/RPi-Distro/pi-gen.git "$WORK_DIR"
fi

# ── Step 3: Prepare pi-gen with SentryUSB config ──
cd "$WORK_DIR"
bash "$SCRIPT_DIR/pi-gen-sources/prepare.sh"

# Use the correct config for the target architecture
cp "$SCRIPT_DIR/pi-gen-sources/$CONFIG_FILE" "$WORK_DIR/config"

# ── Step 4: Inject the pre-built binary and BLE daemon ──
info "Injecting SentryUSB binary into image build..."
cp "$BINARY_PATH" "$WORK_DIR/stage_sentryusb/00-sentryusb-tweaks/files/sentryusb-binary"
chmod +x "$WORK_DIR/stage_sentryusb/00-sentryusb-tweaks/files/sentryusb-binary"

if [ -n "$TESLA_CONTROL_PATH" ] && [ -f "$TESLA_CONTROL_PATH" ]; then
    info "Injecting tesla-control and tesla-keygen..."
    cp "$TESLA_CONTROL_PATH" "$WORK_DIR/stage_sentryusb/00-sentryusb-tweaks/files/tesla-control"
    cp "$TESLA_KEYGEN_PATH"  "$WORK_DIR/stage_sentryusb/00-sentryusb-tweaks/files/tesla-keygen"
    chmod +x "$WORK_DIR/stage_sentryusb/00-sentryusb-tweaks/files/tesla-control"
    chmod +x "$WORK_DIR/stage_sentryusb/00-sentryusb-tweaks/files/tesla-keygen"
fi

info "Injecting BLE daemon files..."
cp "$SCRIPT_DIR/server/ble/sentryusb-ble.py" "$WORK_DIR/stage_sentryusb/00-sentryusb-tweaks/files/sentryusb-ble.py"
cp "$SCRIPT_DIR/server/ble/sentryusb-ble.service" "$WORK_DIR/stage_sentryusb/00-sentryusb-tweaks/files/sentryusb-ble.service"
cp "$SCRIPT_DIR/server/ble/com.sentryusb.ble.conf" "$WORK_DIR/stage_sentryusb/00-sentryusb-tweaks/files/com.sentryusb.ble.conf"

# Trixie apt indices are much larger; increase export image margin
if [[ "$OSTYPE" == darwin* ]]; then
    sed -i '' 's/200 \* 1024 \* 1024/800 * 1024 * 1024/' "$WORK_DIR/export-image/prerun.sh"
else
    sed -i 's/200 \* 1024 \* 1024/800 * 1024 * 1024/' "$WORK_DIR/export-image/prerun.sh"
fi

# ── Step 5: Build the image ──
info "Building image with Docker (this takes 15-30 minutes)..."
./build-docker.sh

# ── Step 6: Copy output ──
IMAGE=$(find "$WORK_DIR/deploy" -name '*.img' | head -1)
if [ -z "$IMAGE" ]; then
    error "Build failed — no image found in deploy/"
fi

mkdir -p "$SCRIPT_DIR/deploy"
info "Compressing image..."
gzip -9 -c "$IMAGE" > "$SCRIPT_DIR/deploy/$(basename "$IMAGE").gz"

ok "Image built successfully!"
echo ""
echo -e "  ${GREEN}Output:${NC} $SCRIPT_DIR/deploy/$(basename "$IMAGE").gz"
echo -e "  ${GREEN}Arch:${NC}   $ARCH_LABEL"
echo ""
echo "  Flash with Raspberry Pi Imager:"
echo "    1. Select 'Use custom' → choose the .img.gz file"
echo "    2. Configure WiFi, hostname (sentryusb), SSH, password in settings"
echo "    3. Write to SD card"
echo ""
echo "  After first boot, open http://sentryusb.local in your browser."
echo "  Or use the SentryUSB iOS app to set up via Bluetooth."
echo ""

# Cleanup
rm -rf "$WORK_DIR"
