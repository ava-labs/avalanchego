 #!/usr/bin/env bash

set -euo pipefail

# This script is shared and should be called from the repository root (graft/coreth or graft/subnet-evm)
# The Taskfile will ensure we're in the correct directory
go tool ginkgo "${@}"

