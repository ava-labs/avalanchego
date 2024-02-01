#!/usr/bin/env bash

git ls-files | grep -v "dependencies/" | grep ".go$" |  xargs gofmt -w
