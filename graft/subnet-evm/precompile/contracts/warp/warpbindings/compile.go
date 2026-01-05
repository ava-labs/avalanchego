// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warpbindings

// Step 1: Compile interface to generate ABI at top level
//go:generate sh -c "solc-v0.8.30 -o .. --overwrite --abi --pretty-json --evm-version cancun IWarpMessenger.sol"
// Step 2: Compile to generate bin files in artifacts
//go:generate solc-v0.8.30 -o artifacts --overwrite --abi --bin --evm-version cancun IWarpMessenger.sol
// Step 3: Generate Go bindings from the compiled artifacts
//go:generate go run github.com/ava-labs/libevm/cmd/abigen --pkg warpbindings --type IWarpMessenger --abi ../IWarpMessenger.abi --bin artifacts/IWarpMessenger.bin --out gen_iwarpmessenger_binding.go
// Step 4: Replace import paths in generated binding to use subnet-evm instead of libevm
// This is necessary because the libevm bindings package is not compatible with the subnet-evm simulated backend, which is used for testing.
//go:generate sh -c "sed -i.bak -e 's|github.com/ava-labs/libevm/accounts/abi|github.com/ava-labs/avalanchego/graft/subnet-evm/accounts/abi|g' -e 's|github.com/ava-labs/libevm/accounts/abi/bind|github.com/ava-labs/avalanchego/graft/subnet-evm/accounts/abi/bind|g' gen_iwarpmessenger_binding.go && rm -f gen_iwarpmessenger_binding.go.bak"
