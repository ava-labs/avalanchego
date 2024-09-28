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
cat >> /etc/default/prometheus-node-exporter <<!
ARGS="--collector.filesystem.mount-points-exclude=\"^/(dev|proc|run|sys|media|var/lib/docker/.+)($|/)\""
!

systemctl daemon-reload
killall grafana-server
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
  mkdir -p "$NVME_MOUNT/ubuntu/firewood"
  chown ubuntu:ubuntu "$NVME_MOUNT/ubuntu" "$NVME_MOUNT/ubuntu/firewood"
  ln -s "$NVME_MOUNT/ubuntu/firewood" /home/ubuntu/firewood
fi

## install go
export GO_INSTALLATION_FILE=go1.23.1.linux-amd64.tar.gz
curl -OL https://go.dev/dl/${GO_INSTALLATION_FILE}
rm -rf /usr/local/go && tar -C /usr/local -xzf ${GO_INSTALLATION_FILE}
rm ${GO_INSTALLATION_FILE}

#### you can switch to the ubuntu user here ####


git clone https://github.com/ava-labs/avalanchego.git
cd avalanchego
git checkout tsachi/bench_merkledb
export PATH=$PATH:/usr/local/go/bin
go build ./x/merkledb/benchmarks_eth

#### stop here, these commands are run by hand ####

# 10M rows:
nohup benchmarks_eth tenkrandom --n 10000000 > output.txt & 
nohup benchmarks_eth zipf --n 10000000 > output.txt &
nohup benchmarks_eth single --n 10000000 > output.txt &

# 50M rows:
nohup benchmarks_eth tenkrandom --n 50000000 > output.txt & 
nohup benchmarks_eth zipf --n 50000000 > output.txt &
nohup benchmarks_eth single --n 50000000 > output.txt &

# 100M rows:
nohup benchmarks_eth tenkrandom --n 100000000 > output.txt & 
nohup benchmarks_eth zipf --n 100000000 > output.txt &
nohup benchmarks_eth single --n 100000000 > output.txt &