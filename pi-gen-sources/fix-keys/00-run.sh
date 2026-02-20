#!/bin/bash -e
# The arm64 pi-gen branch was updated for Trixie (Debian 13), which stores
# the Debian archive keyring as debian-archive-keyring.pgp. When building
# bookworm, the debian-archive-keyring package only installs the .gpg variant.
# debian.sources references the .pgp path via Signed-By:, so APT can't find
# any of the Bookworm signing keys. Creating the .pgp file fixes this.
cp "${ROOTFS_DIR}/usr/share/keyrings/debian-archive-keyring.gpg" \
   "${ROOTFS_DIR}/usr/share/keyrings/debian-archive-keyring.pgp"
