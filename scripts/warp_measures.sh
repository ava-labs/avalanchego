#! /bin/bash
set -eu

avalanche_node_ip="127.0.0.1:9650/"
p_chain_endpoint=$avalanche_node_ip"ext/bc/P"

temp_height_file="/tmp/warp_height_file.json"
cat > "${temp_height_file}" << EOF
{
    "jsonrpc": "2.0",
    "method": "platform.getHeight",
    "params": {},
    "id": 1
}
EOF

height_call=$(curl $p_chain_endpoint -X POST -H 'content-type:application/json' --silent --data "@${temp_height_file}")
if [ $? -ne 0 ]; then
    echo "height call errored. Exiting"
    exit 1
fi

current_height=$(echo $height_call | jq -r '."result"."height"')
echo $current_height

polled_height=$(($current_height - 200))
echo $polled_height

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
echo $validators_call

prometheus_node_addr="127.0.0.1:9090/"
prometheus_endpoint=$prometheus_node_addr"api/v1/query_range?query=rate(avalanche_P_vm_validator_sets_height_diff_sum\[30s\])&range_input=5m&step=1s"

prom_validators_call=$(curl $prometheus_endpoint)
echo $prom_validators_call


# curl 'http://ec2-18-221-44-174.us-east-2.compute.amazonaws.com:9090/api/v1/query_range?query=deriv(avalanche_P_vm_validator_sets_height_diff_sum\[30s\])&start=2023-05-25T23:00:00.000Z&end=2023-05-25T23:59:00.000Z&step=1s' | jq

