#!/usr/bin/env bash
#
# Use lower_case variables in the scripts and UPPER_CASE variables for override
# Use the constants.sh for env overrides
# Use the versions.sh to specify versions
#

# Set up the versions to be used
# Don't export them as their used in the context of other calls
caminoethvm_version=${CAMINOETHVM_VERSION:-'v0.0.0-alpha.1'}
