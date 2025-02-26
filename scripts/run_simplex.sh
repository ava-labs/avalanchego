#!/usr/bin/env bash

set -euo pipefail

AVALANCHE_BINARY="${AVALANCHE_BINARY:-./build/avalanchego} --network-no-ingress-connections-grace-period 9000m  --log-level trace --track-subnets=BKBZ6xXTnT86B4L5fp8rvtcmNSpvtNz8En9jG61ywV2uWyeHy"

pgrep avalanchego && pkill avalanchego

rm -rf local*
rm -rf *.wal

mkdir -p ./local1/plugins
mkdir -p ./local2/plugins
mkdir -p ./local3/plugins
mkdir -p ./local4/plugins
mkdir -p ./local5/plugins

ln /Users/yacov.manevich/.avalanchego/plugins/srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy ./local1/plugins/srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy
ln /Users/yacov.manevich/.avalanchego/plugins/srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy ./local2/plugins/srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy
ln /Users/yacov.manevich/.avalanchego/plugins/srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy ./local3/plugins/srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy
ln /Users/yacov.manevich/.avalanchego/plugins/srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy ./local4/plugins/srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy
ln /Users/yacov.manevich/.avalanchego/plugins/srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy ./local5/plugins/srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy

sleep 3

${AVALANCHE_BINARY} --data-dir ./local1  --network-id=local --db-dir=./local1 --public-ip=127.0.0.1 --http-port=9650 --staking-port=9651 --staking-tls-cert-file=./staking/local/staker1.crt --staking-tls-key-file=./staking/local/staker1.key --staking-signer-key-file=./staking/local/signer1.key --bootstrap-ips="" --bootstrap-ids="" &> node1.log &
${AVALANCHE_BINARY} --data-dir ./local2 --network-id=local --db-dir=./local2 --public-ip=127.0.0.1 --http-port=9660 --staking-port=9661 --staking-tls-cert-file=./staking/local/staker2.crt --staking-tls-key-file=./staking/local/staker2.key --staking-signer-key-file=./staking/local/signer2.key --bootstrap-ips="127.0.0.1:9651" --bootstrap-ids="NodeID-7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg" &> node2.log &
${AVALANCHE_BINARY} --data-dir ./local3 --network-id=local --db-dir=./local3 --public-ip=127.0.0.1 --http-port=9670 --staking-port=9671 --staking-tls-cert-file=./staking/local/staker3.crt --staking-tls-key-file=./staking/local/staker3.key --staking-signer-key-file=./staking/local/signer3.key --bootstrap-ips="127.0.0.1:9651" --bootstrap-ids="NodeID-7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg" &> node3.log &
${AVALANCHE_BINARY} --data-dir ./local4 --network-id=local --db-dir=./local4 --public-ip=127.0.0.1 --http-port=9680 --staking-port=9681 --staking-tls-cert-file=./staking/local/staker4.crt --staking-tls-key-file=./staking/local/staker4.key --staking-signer-key-file=./staking/local/signer4.key --bootstrap-ips="127.0.0.1:9651" --bootstrap-ids="NodeID-7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg" &> node4.log &
${AVALANCHE_BINARY} --data-dir ./local5  --network-id=local --db-dir=./local5 --public-ip=127.0.0.1 --bootstrap-ips="127.0.0.1:9651" --bootstrap-ids="NodeID-7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg" --http-port=9690 --staking-port=9691 --staking-tls-cert-file=./staking/local/staker5.crt --staking-tls-key-file=./staking/local/staker5.key --staking-signer-key-file=./staking/local/signer5.key &> node5.log &








