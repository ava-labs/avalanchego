# syntax=docker/dockerfile:experimental

# This Dockerfile is meant to be used with the build_local_dep_image.sh script
# in order to build an image using the local version of caminoethvm

# Changes to the minimum golang version must also be replicated in
# scripts/ansible/roles/golang_base/defaults/main.yml
# scripts/build_camino.sh
# scripts/local.Dockerfile (here)
# Dockerfile
# README.md
# go.mod
FROM golang:1.17.9-buster

RUN mkdir -p /go/src/github.com/chain4travel

WORKDIR $GOPATH/src/github.com/chain4travel
COPY caminogo caminogo
COPY caminoethvm caminoethvm

WORKDIR $GOPATH/src/github.com/chain4travel/caminogo
RUN ./scripts/build_camino.sh
RUN ./scripts/build_caminoethvm.sh ../caminoethvm $PWD/build/plugins/evm

RUN ln -sv $GOPATH/src/github.com/chain4travel/camino-byzantine/ /caminogo
