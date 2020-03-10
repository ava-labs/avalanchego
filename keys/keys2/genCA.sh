#!/bin/sh
set -ex

openssl genrsa -out `dirname "$0"`/rootCA.key 4096
openssl req -x509 -new -nodes -key `dirname "$0"`/rootCA.key -sha256 -days 365250 -out `dirname "$0"`/rootCA.crt
