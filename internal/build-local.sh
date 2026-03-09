#!/bin/bash -eu
# Local pi-gen build for SentryUSB arm64 image
# Run this inside WSL2 with Docker available

REPO_WIN="/mnt/c/Users/scott/Documents/Sentry-Six-Assets/SentryUSB"
REPO_DIR="/tmp/sentryusb-src"
PIGEN_DIR="/tmp/pi-gen"
ARCH="$(echo "${1:-arm64}" | tr '[:upper:]' '[:lower:]')"

echo "=== SentryUSB local image build (${ARCH}) ==="

# Ensure Docker is running
if ! sudo docker info > /dev/null 2>&1; then
  echo "Starting Docker..."
  sudo service docker start
  sleep 2
fi

# Copy repo sources to native Linux filesystem (avoids CRLF and /mnt/c perf issues)
echo "Copying sources to native filesystem..."
rm -rf "$REPO_DIR"
mkdir -p "$REPO_DIR"
cp -r "$REPO_WIN/pi-gen-sources" "$REPO_DIR/"
cp -r "$REPO_WIN/server/bin" "$REPO_DIR/"

# Fix any Windows line endings
find "$REPO_DIR" -type f \( -name '*.sh' -o -name '*.conf' -o -name 'pi-gen-config*' -o -name 'rc.local' -o -name '00-packages' \) -exec sed -i 's/\r$//' {} +

# Clean previous build
sudo rm -rf "$PIGEN_DIR"

# Clone pi-gen
if [ "$ARCH" = "arm64" ]; then
  echo "Cloning pi-gen arm64 branch..."
  git clone --depth 1 --branch arm64 https://github.com/RPi-Distro/pi-gen.git "$PIGEN_DIR"
else
  echo "Cloning pi-gen master branch (32-bit)..."
  git clone --depth 1 https://github.com/RPi-Distro/pi-gen.git "$PIGEN_DIR"
fi

cd "$PIGEN_DIR"

# Prepare
echo "Running prepare.sh..."
bash "$REPO_DIR/pi-gen-sources/prepare.sh"

# Copy config and binary
if [ "$ARCH" = "arm64" ]; then
  cp "$REPO_DIR/pi-gen-sources/pi-gen-config" config
  cp "$REPO_DIR/bin/sentryusb-linux-arm64" stage_sentryusb/00-sentryusb-tweaks/files/sentryusb-binary
else
  cp "$REPO_DIR/pi-gen-sources/pi-gen-config-32bit" config
  cp "$REPO_DIR/bin/sentryusb-linux-armv7" stage_sentryusb/00-sentryusb-tweaks/files/sentryusb-binary
fi
chmod +x stage_sentryusb/00-sentryusb-tweaks/files/sentryusb-binary

# Trixie apt indices are much larger; increase export image margin
sed -i 's/200 \* 1024 \* 1024/800 * 1024 * 1024/' export-image/prerun.sh

echo "Starting pi-gen build..."
sudo docker rm -v pigen_work 2>/dev/null || true
./build-docker.sh

echo ""
echo "=== Build complete! ==="
echo "Image location:"
find "$PIGEN_DIR/deploy" -name '*.img' 2>/dev/null
