#!/bin/bash

function isBootstrapped() {
    # Check the Info API:
    # https://docs.avax.network/apis/avalanchego/apis/info/#infoisbootstrapped
    BOOTSTRAPPED=$(curl --silent -X POST --data '{
        "jsonrpc":"2.0",
        "id"     :1,
        "method" :"info.isBootstrapped",
        "params": {
            "chain":"'"${1}"'"
        }
    }' -H 'content-type:application/json;' 127.0.0.1:9650/ext/info | jq .result.isBootstrapped)

    echo "${BOOTSTRAPPED}"
}

# Check the Health API:
# https://docs.avax.network/apis/avalanchego/apis/health/
HEALTHY=$(curl --silent -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"health.health"
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/health | jq .result.healthy)
if [ "${HEALTHY}" = "true" ]; then
    exit 0
fi

# Consider bootstrapping chains as healthy, even if the Health API says otherwise.
if [ "$(isBootstrapped 'P')" = "false" ] || \
   [ "$(isBootstrapped 'X')" = "false" ] || \
   [ "$(isBootstrapped 'C')" = "false" ]; then
    exit 0
fi

exit 1
