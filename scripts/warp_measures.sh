#! /bin/bash
set -eu

# TODO:
# Just make a bunch of calls, recording timestamp and height called
# Calls may be alternated among height and getValidatorsAt
# All the other data can be retrieved from prometheus and processed by hand.

avalanche_node_addr="127.0.0.1:9650/"
p_chain_endpoint=$avalanche_node_addr"ext/bc/P"

temp_height_file="/tmp/warp_height_file.json"
cat > "${temp_height_file}" << EOF
{
    "jsonrpc": "2.0",
    "method": "platform.getHeight",
    "params": {},
    "id": 1
}
EOF

timestamp=$(date +%s)
echo $timestamp "START"

for depth in {10000..1..50}; do
height_call=$(curl $p_chain_endpoint -X POST -H 'content-type:application/json' --silent --data "@${temp_height_file}")
if [ $? -ne 0 ]; then
    echo "height call errored. Exiting"
    exit 1
fi

current_height=$(echo $height_call | jq -r '."result"."height"')
timestamp=$(date +%s)
echo $timestamp "current  height" $current_height

polled_height=$(($current_height - depth))

temp_validatorsAt_file="/tmp/warp_validators_at_file.json"
cat > "${temp_validatorsAt_file}" << EOF
{
    "jsonrpc": "2.0",
    "method": "platform.getValidatorsAt",
    "params": {
        "height":$polled_height
    },
    "id": 1
}
EOF

validators_call=$(curl $p_chain_endpoint -X POST -H 'content-type:application/json' --silent --data "@${temp_validatorsAt_file}")
if [ $? -ne 0 ]; then
    echo "validators at call errored. Exiting"
    exit 1
fi
timestamp=$(date +%s)
echo $timestamp "validtrs height" $polled_height

sleep 30
done

timestamp=$(date +%s)
echo $timestamp "DONE"
