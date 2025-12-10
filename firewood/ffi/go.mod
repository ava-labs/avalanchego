module github.com/ava-labs/firewood/ffi

go 1.24

// Changes to the toolchain version should be replicated in:
//   - ffi/go.mod (here)
//   - ffi/flake.nix (update golang.url to a version of avalanchego's nix/go/flake.nix that uses the desired version and run `just update-ffi-flake`)
//   - ffi/tests/eth/go.mod
//   - ffi/tests/firewood/go.mod
// `just check-golang-version` validates that these versions are in sync and will run in CI as part of the ffi-nix job.
toolchain go1.24.9

require (
	github.com/prometheus/client_golang v1.22.0
	github.com/prometheus/client_model v0.6.1
	github.com/prometheus/common v0.62.0
	github.com/stretchr/testify v1.10.0
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	golang.org/x/sys v0.30.0 // indirect
	google.golang.org/protobuf v1.36.5 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
