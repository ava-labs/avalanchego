// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bindings

// Step 1: Compile interface to generate ABI at top level
//go:generate solc -o ../.. --overwrite --abi --base-path ../../../../.. --pretty-json --evm-version cancun ../../IGasPriceManager.sol
// Step 2: Generate Go bindings from the compiled artifacts
//go:generate go run github.com/ava-labs/libevm/cmd/abigen --pkg bindings --type IGasPriceManager --abi ../../IGasPriceManager.abi --out gen_igaspricemanager_binding.go
// Step 3: Replace import paths in generated binding to use subnet-evm instead of libevm
// This is necessary because the libevm bindings package is not compatible with the subnet-evm simulated backend, which is used for testing.
//go:generate sh -c "sed -i.bak -e 's|github.com/ava-labs/libevm/accounts/abi/bind|github.com/ava-labs/avalanchego/graft/subnet-evm/accounts/abi/bind|g' gen_igaspricemanager_binding.go && rm -f gen_igaspricemanager_binding.go.bak"
