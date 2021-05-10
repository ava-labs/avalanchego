#!/usr/bin/env bash


# Set up the versions to be used
# Don't export them as their used in the context of other calls
coreth_version=${CORETH_VERSION:-'v0.5.0-rc.2'}
go_ethereum=${GO_ETHEREUM:-'v1.9.21'}
