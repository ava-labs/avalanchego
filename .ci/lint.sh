# binary will be $(go env GOPATH)/bin/golangci-lint
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b "$(go env GOPATH)"/bin latest

"$(go env GOPATH)"/bin/golangci-lint --version

"$(go env GOPATH)"/bin/golangci-lint run --max-same-issues=0 --timeout=2m
