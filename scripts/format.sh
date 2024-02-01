#!/usr/bin/env bash

git ls-files | grep ".go$" |  xargs gofmt -w
