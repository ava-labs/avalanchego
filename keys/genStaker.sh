#!/bin/sh 
set -e

src_dir=$(cd $(dirname $0) && pwd)

if test -f "${src_dir}/staker.key" || test -f "${src_dir}/staker.crt"; then
    echo "${src_dir}/staker.key or ${src_dir}/staker.crt already exists. Not generating new key/certificiate."
    exit 1
fi

openssl genrsa -out `dirname "$0"`/staker.key 4096
openssl req -new -sha256 -key `dirname "$0"`/staker.key -subj "/C=US/ST=NY/O=Avalabs/CN=ava" -out `dirname "$0"`/staker.csr
openssl x509 -req -in `dirname "$0"`/staker.csr -CA `dirname "$0"`/rootCA.crt -CAkey `dirname "$0"`/rootCA.key -CAcreateserial -out `dirname "$0"`/staker.crt -days 365250 -sha256

if test -f "${src_dir}/staker.key" && test -f "${src_dir}/staker.crt"; then
    echo "Staking key/certificate generated successfully"
else
    echo "Error: staking key/certificate not generated"
fi
