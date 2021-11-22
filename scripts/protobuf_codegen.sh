#!/bin/bash

if ! [[ "$0" =~ scripts/protobuf_codegen.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

TARGET=$PWD
if [ -n "$1" ]; then 
  TARGET="$1"
fi

echo "Re-genrating protobuf for target directory: $TARGET"

# go module does not download the actual "io/prometheus/client/metrics.proto" file
# manually download using git
PROMETHEUS_PATH=/tmp/client_model
rm -rf ${PROMETHEUS_PATH}
git clone --quiet -b v0.2.0 https://github.com/prometheus/client_model.git ${PROMETHEUS_PATH}

for PROTO_BUFFER_FILE in $(find $TARGET -type f ! -path '*/.git/*' | grep proto$)
do
  echo "Re-generating protobuf: $PROTO_BUFFER_FILE"
  PROTO_FILE_DIR=$(dirname $PROTO_BUFFER_FILE)
  protoc \
    --proto_path="$PROMETHEUS_PATH" \
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

rm -rf ${PROMETHEUS_PATH}
