#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

for i in `ls -d *`
do
	if [ ! -d "${i}" ]
	then
		continue
	fi
	go test -race -timeout="20m" -coverprofile="coverage.out" -covermode="atomic" ./${i}/... && exit $?
done
