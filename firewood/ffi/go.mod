module github.com/ava-labs/firewood/ffi

// Changes to the go version should be replicated in:
//   - ffi/go.mod (here)
//   - ffi/tests/eth/go.mod
//   - ffi/tests/firewood/go.mod
go 1.25.10

require (
	github.com/prometheus/client_golang v1.22.0
	github.com/prometheus/client_model v0.6.1
	github.com/stretchr/testify v1.10.0
	github.com/vmihailenco/msgpack/v5 v5.4.1
	golang.org/x/sync v0.19.0
	golang.org/x/tools v0.42.0
	google.golang.org/protobuf v1.36.5
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/common v0.62.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	golang.org/x/mod v0.33.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
