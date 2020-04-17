# syntax=docker/dockerfile:experimental

FROM golang:1.13.4-buster

RUN apt-get update && apt-get install -y libssl-dev libuv1-dev curl cmake

RUN mkdir -p /go/src/github.com/ava-labs

WORKDIR $GOPATH/src/github.com/ava-labs/
COPY . gecko

WORKDIR $GOPATH/src/github.com/ava-labs/gecko
RUN ./scripts/build.sh
