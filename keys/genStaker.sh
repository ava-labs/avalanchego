#!/bin/sh
set -ex

keypath=$GOPATH/src/github.com/ava-labs/gecko/keys

if test -f "$keypath/staker.key" || test -f "$keypath/staker.crt"; then
    echo "staker.key or staker.crt already exists. Not generating new key/certificiate."
    exit 1
fi

openssl genrsa -out `dirname "$0"`/staker.key 4096
openssl req -new -sha256 -key `dirname "$0"`/staker.key -subj "/C=US/ST=NY/O=Avalabs/CN=ava" -out `dirname "$0"`/staker.csr
openssl x509 -req -in `dirname "$0"`/staker.csr -CA `dirname "$0"`/rootCA.crt -CAkey `dirname "$0"`/rootCA.key -CAcreateserial -out `dirname "$0"`/staker.crt -days 365250 -sha256
