#!/usr/bin/env bash

# Ignore warnings about variables appearing unused since this file is not the consumer of the variables it defines.
# shellcheck disable=SC2034

set -euo pipefail

# Don't export them as they're used in the context of other calls
AVALANCHE_VERSION=${AVALANCHE_VERSION:-'ab83fb41528de93c1790301cdd67a07dda9299f0'}
