#!/bin/bash
# This script sets up the build environment, including installing the firewood build dependencies.
set -o errexit

if [ "$EUID" -ne 0 ]; then
    echo "This script must be run as root" >&2
    exit 1
fi

apt upgrade -y

# install the build dependency packages
pkgs=(git protobuf-compiler build-essential apt-transport-https net-tools zfsutils-linux)
install_pkgs=()
for pkg in "${pkgs[@]}"; do
  if ! dpkg -s "$pkg" > /dev/null 2>&1; then
    install_pkgs+=("$pkg")
  fi
done
if [ "${#install_pkgs[@]}" -gt 0 ]; then
  apt-get install -y "${install_pkgs[@]}"
fi
  
# If there is an NVMe device, format it and mount it to /mnt/nvme/ubuntu/firewood
# this happens on amazon ec2 instances
NVME_DEV="$(realpath /dev/disk/by-id/nvme-Amazon_EC2_NVMe_Instance_Storage_* | uniq)"
if [ -n "$NVME_DEV" ]; then
  mkfs.ext4 -E nodiscard -i 6291456 "$NVME_DEV"
  NVME_MOUNT=/mnt/nvme
  mkdir -p "$NVME_MOUNT"
  mount -o noatime "$NVME_DEV" "$NVME_MOUNT"
  echo "$NVME_DEV $NVME_MOUNT ext4 noatime 0 0" >> /etc/fstab
  mkdir -p "$NVME_MOUNT/ubuntu/firewood"
  chown ubuntu:ubuntu "$NVME_MOUNT/ubuntu" "$NVME_MOUNT/ubuntu/firewood"
  ln -s "$NVME_MOUNT/ubuntu/firewood" /home/ubuntu/firewood
fi
