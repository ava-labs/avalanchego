# syntax=docker/dockerfile:experimental

FROM golang:1.13.4-buster

RUN mkdir -p /go/src/github.com/ava-labs

WORKDIR $GOPATH/src/github.com/ava-labs/
COPY . gecko

WORKDIR $GOPATH/src/github.com/ava-labs/gecko
RUN ./scripts/build.sh

RUN ln -sv $GOPATH/src/github.com/ava-labs/gecko/ /gecko
