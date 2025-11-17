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
  elif [ "${#NVME_DEVS[@]}" -eq 2 ]; then
    # Two devices, create RAID1
    echo "Creating RAID1 array with 2 devices: ${NVME_DEVS[*]}"
    mdadm --create /dev/md0 --level=1 --raid-devices=2 "${NVME_DEVS[@]}"
    DEVICE_TO_USE="/dev/md0"
  elif [ "${#NVME_DEVS[@]}" -eq 3 ]; then
    # Three devices, create RAID5
    echo "Creating RAID5 array with 3 devices: ${NVME_DEVS[*]}"
    mdadm --create /dev/md0 --level=5 --raid-devices=3 "${NVME_DEVS[@]}"
    DEVICE_TO_USE="/dev/md0"
  elif [ "${#NVME_DEVS[@]}" -eq 4 ]; then
    # Four devices, create RAID10
    echo "Creating RAID10 array with 4 devices: ${NVME_DEVS[*]}"
    mdadm --create /dev/md0 --level=10 --raid-devices=4 "${NVME_DEVS[@]}"
    DEVICE_TO_USE="/dev/md0"
  else
    echo "Unsupported number of NVMe devices: ${#NVME_DEVS[@]}. Using first device only."
    DEVICE_TO_USE="${NVME_DEVS[0]}"
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
