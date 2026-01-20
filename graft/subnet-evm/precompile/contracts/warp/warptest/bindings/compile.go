// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bindings

// Step 1: Compile Solidity contract to generate ABI and bin files
// Uses base-path to resolve imports from the repo root
//go:generate solc -o artifacts --overwrite --abi --bin --base-path ../../../../.. WarpTest.sol
// Step 2: Generate Go bindings from the compiled artifacts
// WarpTest binding includes WarpMessage and WarpBlockHash struct definitions.
// For event filtering, use the IWarpMessenger binding from the warpbindings package.
//go:generate go run github.com/ava-labs/libevm/cmd/abigen --pkg bindings --type WarpTest --abi artifacts/WarpTest.abi --bin artifacts/WarpTest.bin --out gen_warptest_binding.go
// Step 3: Replace import paths in generated binding to use subnet-evm instead of libevm
// This is necessary because the libevm bindings package is not compatible with the subnet-evm simulated backend, which is used for testing.
//go:generate sh -c "sed -i.bak -e 's|github.com/ava-labs/libevm/accounts/abi|github.com/ava-labs/avalanchego/graft/subnet-evm/accounts/abi|g' -e 's|github.com/ava-labs/libevm/accounts/abi/bind|github.com/ava-labs/avalanchego/graft/subnet-evm/accounts/abi/bind|g' gen_warptest_binding.go && rm -f gen_warptest_binding.go.bak"
