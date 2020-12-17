# binary will be $(go env GOPATH)/bin/golangci-lint
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v1.30.0

export PATH=$PATH:$(go env GOPATH)/bin

golangci-lint --version

golangci-lint run --max-same-issues 0
