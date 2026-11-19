#!/usr/bin/env -eux bash

set -o pipefail

LOG_LEVEL=info go test -v -race ./... &> out.log

if [[ $? -ne 0 ]];then
  echo "Tests failed"
  cat out.log
  exit 1
fi

cat out.log

echo "Checking for warnings or errors in the test output"

grep -Eq "ERR|WARN" out.log 
if [[ $? -eq 0 ]];then
  echo ""
  echo ""
	echo " ------------------------- Found warnings or errors in the test run -------------------------"
	echo ""
	echo ""
	exit 1
fi

echo "No warnings or errors found in the test run"
exit 0

