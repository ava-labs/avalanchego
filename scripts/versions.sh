#!/usr/bin/env bash
#
# Use lower_case variables in the scripts and UPPER_CASE variables for override
# Use the constants.sh for env overrides
# Use the versions.sh to specify versions
#

# Set up the versions to be used
# Don't export them as their used in the context of other calls
coreth_version=${CORETH_VERSION:-'v0.5.6-rc.2'}
# Release of AvalancheGo compatible with previous database version
prev_avalanchego_version=${PREV_AVALANCHEGO_VERSION:-'v1.4.5-preupgrade.4' }
