#!/bin/bash
# This script sets up the build environment, including installing the firewood build dependencies.
set -o errexit

if [ "$EUID" -ne 0 ]; then
    echo "This script must be run as root" >&2
    exit 1
fi

# Default bytes-per-inode for ext4 filesystem (2MB)
BYTES_PER_INODE=2097152

# Parse command line arguments
show_usage() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  --bytes-per-inode BYTES  Set bytes-per-inode for ext4 filesystem (default: 2097152)"
    echo "  --help                   Show this help message"
}

while [[ $# -gt 0 ]]; do
    case $1 in
        --bytes-per-inode)
            BYTES_PER_INODE="$2"
            shift 2
            ;;
        --help)
            show_usage
            exit 0
            ;;
        *)
            echo "Error: Unknown option $1" >&2
            show_usage
            exit 1
            ;;
    esac
done

apt upgrade -y

# install the build dependency packages
pkgs=(git protobuf-compiler build-essential apt-transport-https net-tools zfsutils-linux mdadm)
install_pkgs=()
for pkg in "${pkgs[@]}"; do
  if ! dpkg -s "$pkg" > /dev/null 2>&1; then
    install_pkgs+=("$pkg")
  fi
done
if [ "${#install_pkgs[@]}" -gt 0 ]; then
  apt-get install -y "${install_pkgs[@]}"
fi
  
# If there are NVMe devices, set up RAID if multiple, or use single device
mapfile -t NVME_DEVS < <(realpath /dev/disk/by-id/nvme-Amazon_EC2_NVMe_Instance_Storage_* 2>/dev/null | sort | uniq)
if [ "${#NVME_DEVS[@]}" -gt 0 ]; then
  DEVICE_TO_USE=""

  if [ "${#NVME_DEVS[@]}" -eq 1 ]; then
    # Single device, use it directly
    DEVICE_TO_USE="${NVME_DEVS[0]}"
    echo "Using single NVMe device: $DEVICE_TO_USE"
  else
    # Multiple devices, create RAID0
    echo "Creating RAID0 array with ${#NVME_DEVS[@]} devices: ${NVME_DEVS[*]}"
    mdadm --create /dev/md0 --level=0 --raid-devices="${#NVME_DEVS[@]}" "${NVME_DEVS[@]}"
    DEVICE_TO_USE="/dev/md0"
  fi

  # Wait for RAID array to be ready (if created)
  if [[ "$DEVICE_TO_USE" == "/dev/md0" ]]; then
    echo "Waiting for RAID array to be ready..."
    while [ ! -e "$DEVICE_TO_USE" ]; do
      sleep 1
    done
    # Save RAID configuration
    mdadm --detail --scan >> /etc/mdadm/mdadm.conf
    update-initramfs -u
  fi

  # Format and mount the device
  mkfs.ext4 -E nodiscard -i "$BYTES_PER_INODE" "$DEVICE_TO_USE"
  NVME_MOUNT=/mnt/nvme
  mkdir -p "$NVME_MOUNT"
  mount -o noatime "$DEVICE_TO_USE" "$NVME_MOUNT"
  echo "$DEVICE_TO_USE $NVME_MOUNT ext4 noatime 0 0" >> /etc/fstab
  mkdir -p "$NVME_MOUNT/ubuntu/firewood"
  chown ubuntu:ubuntu "$NVME_MOUNT/ubuntu" "$NVME_MOUNT/ubuntu/firewood"
  ln -s "$NVME_MOUNT/ubuntu/firewood" /home/ubuntu/firewood
fi
