#!/usr/bin/env bash

# Ignore warnings about variables appearing unused since this file is not the consumer of the variables it defines.
# shellcheck disable=SC2034

set -euo pipefail

# Don't export them as they're used in the context of other calls
avalanche_version=${AVALANCHE_VERSION:-'v1.11.2-rc.8'}
