#!/bin/bash

# Ted: contact me when you make any changes

# resolve the required env for salticidae-go
GOPATH="$(go env GOPATH)"
SALTICIDAE_GO_HOME="$GOPATH/src/github.com/ava-labs/salticidae-go/"

if [[ -f "$SALTICIDAE_GO_HOME/salticidae/libsalticidae.a" ]]; then
    source "$SALTICIDAE_GO_HOME/scripts/env.sh"
else
    source /dev/stdin <<<"$(curl -sS https://raw.githubusercontent.com/ava-labs/salticidae-go/v0.1.0/setup.sh)"
fi
