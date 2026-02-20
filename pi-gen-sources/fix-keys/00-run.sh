#!/bin/bash -e
# Temporarily allow unauthenticated repos so 01-run-chroot.sh can install
# the current debian-archive-keyring. Removed by 01-run-chroot.sh.
echo 'APT::Get::AllowUnauthenticated "true";' \
    > "${ROOTFS_DIR}/etc/apt/apt.conf.d/00insecure"
echo 'Acquire::AllowInsecureRepositories "true";' \
    >> "${ROOTFS_DIR}/etc/apt/apt.conf.d/00insecure"
