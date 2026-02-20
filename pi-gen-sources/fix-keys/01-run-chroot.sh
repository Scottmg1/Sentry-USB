#!/bin/bash -e
# Install the current debian-archive-keyring so all subsequent apt-get
# calls can verify Debian Bookworm signatures normally.
# The insecure-repo config written by 00-run.sh is removed at the end.
apt-get update --allow-insecure-repositories
apt-get install -y --allow-unauthenticated debian-archive-keyring
rm /etc/apt/apt.conf.d/00insecure
