#!/bin/bash -e

# Fill environment
GECKO_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd ) # Directory above this script
source ${GECKO_PATH}/scripts/env.sh

# Remove binaries
rm -r ${BUILD_DIR}/*

# Call salticidae-go's clean script
sh $SALTICIDAE_GO_PATH/scripts/clean.sh