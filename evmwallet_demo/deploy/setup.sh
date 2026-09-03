#!/bin/bash
# One-shot installer, run as root on a fresh Amazon Linux 2023 box after
# launch.sh copied this directory (evmwallet_demo), the prebuilt binaries and
# the cloudflared tunnel credentials to /opt/evmwallet/src and
# /opt/evmwallet/tunnel.json.
#
# Layout after install:
#   /opt/evmwallet/bin/{avalanchego,bootstrap}
#   /opt/evmwallet/www          demo page
#   /opt/evmwallet/deploy       these scripts
#   /var/lib/evmwallet/network.json   written by bootstrap, read by the proxy
set -euo pipefail

SRC=/opt/evmwallet/src
HOSTNAME=cchain-evm-wallet.containerman.me

id -u evmwallet >/dev/null 2>&1 || useradd -r -m -d /var/lib/evmwallet evmwallet
mkdir -p /opt/evmwallet/bin /opt/evmwallet/www /var/lib/evmwallet
install -m 755 $SRC/avalanchego $SRC/bootstrap /opt/evmwallet/bin/
cp $SRC/evmwallet_demo/www/* /opt/evmwallet/www/
rm -rf /opt/evmwallet/deploy && cp -r $SRC/evmwallet_demo/deploy /opt/evmwallet/deploy
chown -R evmwallet:evmwallet /var/lib/evmwallet /opt/evmwallet/www

# cloudflared: named tunnel, ingress -> proxy on 8080
if ! command -v cloudflared >/dev/null; then
  curl -fsSL -o /usr/local/bin/cloudflared \
    https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64
  chmod +x /usr/local/bin/cloudflared
fi
mkdir -p /etc/cloudflared
cp /opt/evmwallet/tunnel.json /etc/cloudflared/tunnel.json
TUNNEL_ID=$(python3 -c 'import json;print(json.load(open("/etc/cloudflared/tunnel.json"))["TunnelID"])')
cat >/etc/cloudflared/config.yml <<CFG
tunnel: $TUNNEL_ID
credentials-file: /etc/cloudflared/tunnel.json
ingress:
  - hostname: $HOSTNAME
    service: http://localhost:8080
  - service: http_status:404
CFG
cloudflared service install 2>/dev/null || true

cp /opt/evmwallet/deploy/*.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now evmwallet-network evmwallet-proxy cloudflared
systemctl restart evmwallet-network evmwallet-proxy cloudflared
echo "install done; tail -f /var/lib/evmwallet/bootstrap.log for network progress"
