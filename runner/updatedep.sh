#!/usr/bin/env bash
set -e

if ! [[ "$0" =~ updatedep.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

go mod tidy -v -compat=1.17
