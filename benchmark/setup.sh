mkdir -p /etc/apt/keyrings/
wget -q -O - https://apt.grafana.com/gpg.key | gpg --dearmor | sudo tee /etc/apt/keyrings/grafana.gpg > /dev/null
echo "deb [signed-by=/etc/apt/keyrings/grafana.gpg] https://apt.grafana.com stable main" | sudo tee -a /etc/apt/sources.list.d/grafana.list
apt-get update
apt-get upgrade -y
  
mkdir -p /etc/systemd/system/grafana-server.service.d
cat > /etc/systemd/system/grafana-server.service.d/override.conf <<!
[Service]
# Give the CAP_NET_BIND_SERVICE capability
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
AmbientCapabilities=CAP_NET_BIND_SERVICE
  
# A private user cannot have process capabilities on the host's user
# namespace and thus CAP_NET_BIND_SERVICE has no effect.
PrivateUsers=false
!
  
apt install -y git protobuf-compiler build-essential apt-transport-https grafana prometheus net-tools
  
perl -pi -e 's/^;?http_port = .*/http_port = 80/' /etc/grafana/grafana.ini
cat >> /etc/prometheus/prometheus.yml <<!
  - job_name: firewood
    static_configs:
      - targets: ['localhost:3000']
!
  
systemctl daemon-reload
systemctl start grafana-server
systemctl enable grafana-server.service
systemctl restart prometheus
  
NVME_DEV="$(realpath /dev/disk/by-id/nvme-Amazon_EC2_NVMe_Instance_Storage_* | uniq)"
if [ -n "$NVME_DEV" ]; then
  mkfs.ext4 -E nodiscard -i 6291456 "$NVME_DEV"
  NVME_MOUNT=/mnt/nvme
  mkdir -p "$NVME_MOUNT"
  mount -o noatime "$NVME_DEV" "$NVME_MOUNT"
  echo "$NVME_DEV $NVME_MOUNT ext4 noatime 0 0" >> /etc/fstab
  mkdir "$NVME_MOUNT/ubuntu"
  chown ubuntu:ubuntu "$NVME_MOUNT/ubuntu"
  ln -s /home/ubuntu/firewood "$NVME_MOUNT/ubuntu/firewood"
fi
  
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
. "$HOME/.cargo/env"
git clone https://github.com/ava-labs/firewood.git
cd firewood
git checkout rkuris/prometheus
cargo build --release

# nohup time cargo run --release --bin benchmark -- -b 10000 -c 1500000 -n 100000 &
nohup time cargo run --release --bin benchmark -- -b 100000 -c 1500000 -n 1000 -i &
