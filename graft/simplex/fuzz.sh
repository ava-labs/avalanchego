#!/usr/bin/env -euxo pipefail bash

rand=$RANDOM
x=$(( $rand % 10 ))
if [[ $x -ne 0 ]]; then
  echo "Skipping fuzz run"
  exit 0
fi

grep -ore "Fuzz.*(f \*" * | cut -d\( -f1 | while read line; do
  file=$( echo $line | cut -d: -f1 )
  testName=$( echo $line | cut -d: -f2  )
  dir=$( dirname $file )
  cd $dir
  LOG_LEVEL=info go test -fuzz="${testName}" -fuzztime 10s
  cd -
done
