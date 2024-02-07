#!/usr/bin/env bash

# Ignore warnings about variables appearing unused since this file is not the consumer of the variables it defines.
# shellcheck disable=SC2034

set -euo pipefail

# Don't export them as they're used in the context of other calls
avalanche_version=${AVALANCHE_VERSION:-'e248179ae75918581fec77ba09fd1ca939bb1844'}
