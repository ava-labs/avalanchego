#!/bin/bash

TARGET=$PWD

if [ -n "$1" ]; then 
  TARGET="$1"
fi

echo "Re-genrating protobuf for target directory: $TARGET"

for PROTO_BUFFER_FILE in $(find $TARGET -type f | grep proto$)
do
  echo "Re-generating protobuf: $PROTO_BUFFER_FILE"
  PROTO_FILE_DIR=$(dirname $PROTO_BUFFER_FILE)
  protoc \
    --fatal_warnings \
    -I="$PROTO_FILE_DIR" \
    --go_out="$PROTO_FILE_DIR" \
    --go-grpc_out="$PROTO_FILE_DIR" \
    --go_opt=paths=source_relative \
    --go-grpc_opt=paths=source_relative \
    "$PROTO_BUFFER_FILE"
  if [[ $? -ne 0 ]];  then
    echo "WARN: protobuf codegen did not succeed for $PROTO_BUFFER_FILE"
  else
    echo "protobuf codegen succeed for $PROTO_BUFFER_FILE"
  fi
done
