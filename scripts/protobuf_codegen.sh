#!/bin/bash

TARGET="."

if [ -n "$1" ]; then 
  TARGET="$1"
fi

echo "Target folder is: $TARGET"

for PBF in $(find $TARGET -type f | grep proto$)
do
  echo $PBF
  PFILE=$(basename $PBF)
  FDIR=$(dirname $PBF)
  PDIR=$(dirname $FDIR)
  PROTODIR=$(basename $FDIR)
  protoc\
    -I="$FDIR"\
    --go_out="$FDIR"\
    --go-grpc_out="$PDIR"\
    --go_opt=paths=source_relative\
    --go-grpc_opt M"$PFILE"=/"$PROTODIR"\
    "$PBF"
  if [[ $? -ne 0 ]];  then
    echo "WARN: protobuf codegen did not succeed for $PBF"
  else
    echo "protobuf codegen succeed for $PBF"
  fi
done

