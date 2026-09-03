#!/bin/bash
# Launch the demo box in ap-northeast-1 from kun and install everything.
# Needs a live `aws sso login --sso-session development`. Ships prebuilt
# linux/amd64 binaries (built by build.sh) so the box does not compile.
# `--no-launch` reinstalls on the existing box instead of creating one.
set -euo pipefail
cd "$(dirname "$0")/.."

REGION=ap-northeast-1
NAME=ilya-solohin-evmwallet-demo-2026-09
MYIP=$(curl -s ifconfig.me)
TAGS="Tags=[{Key=Name,Value=$NAME},{Key=Owner,Value=ilya.solohin@avalabs.org},{Key=Project,Value=cchain-evm-wallet}]"
PEM=~/.ssh/epochdb_tokyo.pem
BIN=${BIN:-/tmp/evmwallet-demo-bin}
TUNNEL_JSON=${TUNNEL_JSON:?path to the cloudflared tunnel credentials json}

SG=$(aws ec2 describe-security-groups --region $REGION --filters "Name=group-name,Values=$NAME" \
  --query 'SecurityGroups[0].GroupId' --output text 2>/dev/null)
if [ "$SG" = "None" ] || [ -z "$SG" ]; then
  SG=$(aws ec2 create-security-group --region $REGION --group-name $NAME --description "evmwallet demo, ssh from kun only" \
    --tag-specifications "ResourceType=security-group,$TAGS" --query GroupId --output text)
  aws ec2 authorize-security-group-ingress --region $REGION --group-id $SG --protocol tcp --port 22 --cidr $MYIP/32 \
    --tag-specifications "ResourceType=security-group-rule,$TAGS" >/dev/null
fi
echo "sg $SG (22 from $MYIP only)"

AMI=$(aws ssm get-parameter --region $REGION \
  --name /aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-x86_64 \
  --query 'Parameter.Value' --output text)

if [ "${1:-}" != "--no-launch" ]; then
  ID=$(aws ec2 run-instances --region $REGION --image-id "$AMI" --instance-type t3.medium --count 1 \
    --key-name ilya-solohin-epochdb-key --security-group-ids $SG \
    --block-device-mappings 'DeviceName=/dev/xvda,Ebs={VolumeSize=30,VolumeType=gp3}' \
    --tag-specifications "ResourceType=instance,$TAGS" "ResourceType=volume,$TAGS" "ResourceType=network-interface,$TAGS" \
    --query 'Instances[0].InstanceId' --output text)
  echo "instance $ID"
  aws ec2 wait instance-running --region $REGION --instance-ids "$ID"
else
  ID=$(aws ec2 describe-instances --region $REGION --filters "Name=tag:Name,Values=$NAME" "Name=instance-state-name,Values=running" \
    --query 'Reservations[0].Instances[0].InstanceId' --output text)
fi
IP=$(aws ec2 describe-instances --region $REGION --instance-ids "$ID" \
  --query 'Reservations[0].Instances[0].PublicIpAddress' --output text)
echo "ip $IP"

SSH="ssh -i $PEM -o StrictHostKeyChecking=no -o ConnectTimeout=5 ec2-user@$IP"
until $SSH true 2>/dev/null; do sleep 5; done
$SSH 'sudo mkdir -p /opt/evmwallet/src && sudo chown -R ec2-user /opt/evmwallet'
scp -i "$PEM" -o StrictHostKeyChecking=no -r . "ec2-user@$IP:/opt/evmwallet/src/evmwallet_demo"
scp -i "$PEM" -o StrictHostKeyChecking=no "$BIN/avalanchego" "$BIN/bootstrap" "ec2-user@$IP:/opt/evmwallet/src/"
scp -i "$PEM" -o StrictHostKeyChecking=no "$TUNNEL_JSON" "ec2-user@$IP:/opt/evmwallet/tunnel.json"
$SSH 'sudo bash /opt/evmwallet/src/evmwallet_demo/deploy/setup.sh'
echo "done: https://cchain-evm-wallet.containerman.me  (ssh: $SSH)"
