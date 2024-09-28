#### run these commands from the root user ####

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
  ln -s "$NVME_MOUNT/ubuntu/firewood" /home/ubuntu/firewood
fi


#### you can switch to the ubuntu user here ####
  
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
. "$HOME/.cargo/env"
git clone https://github.com/ava-labs/firewood.git
cd firewood
git checkout rkuris/prometheus
cargo build --profile maxperf

# 10M rows:
nohup time cargo run --profile maxperf --bin benchmark -- -n 1000 create
nohup time cargo run --profile maxperf --bin benchmark -- -n 1000 zipf
nohup time cargo run --profile maxperf --bin benchmark -- -n 1000 single

# 50M rows:
nohup time cargo run --profile maxperf --bin benchmark -- -n 5000 create
nohup time cargo run --profile maxperf --bin benchmark -- -n 5000 zipf
nohup time cargo run --profile maxperf --bin benchmark -- -n 5000 single

# 100M rows:
nohup time cargo run --profile maxperf --bin benchmark -- -n 10000 create
nohup time cargo run --profile maxperf --bin benchmark -- -n 10000 zipf
nohup time cargo run --profile maxperf --bin benchmark -- -n 10000 single
