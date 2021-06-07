#!/usr/bin/env bash

# Set up the versions to be used
# Don't export them as their used in the context of other calls
coreth_version=${CORETH_VERSION:-'v0.5.4-rc.1'}
go_ethereum=${GO_ETHEREUM:-'v1.9.21'}
 # Release of AvalancheGo compatible with previous database version
prev_avalanchego_version=${PREV_AVALANCHEGO_VERSION:-'v1.4.5-preupgrade.4' }
