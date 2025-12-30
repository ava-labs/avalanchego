// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bindings

// Step 1: Compile interface to generate ABI at top level
//go:generate sh -c "solc-v0.8.30 -o ../.. --overwrite --abi --pretty-json --evm-version cancun ../../IAllowList.sol"
// Step 2: Compile test contracts to generate ABI and bin files
//go:generate solc-v0.8.30 -o artifacts --overwrite --abi --bin --base-path . --metadata-hash none precompile/=../../../ --evm-version cancun AllowListTest.sol
// Step 3: Generate Go bindings from the compiled artifacts
//go:generate go run github.com/ava-labs/libevm/cmd/abigen --pkg bindings --type IAllowList --abi ../../IAllowList.abi --bin artifacts/IAllowList.bin --out gen_allowlist_binding.go
//go:generate go run github.com/ava-labs/libevm/cmd/abigen --pkg bindings --type AllowListTest --abi artifacts/AllowListTest.abi --bin artifacts/AllowListTest.bin --out gen_allowlisttest_binding.go
// Step 4: Replace import paths in generated binding to use subnet-evm instead of libevm
// This is necessary because the libevm bindings package is not compatible with the subnet-evm simulated backend, which is used for testing.
//go:generate sh -c "sed -i.bak -e 's|github.com/ava-labs/libevm/accounts/abi|github.com/ava-labs/avalanchego/graft/subnet-evm/accounts/abi|g' -e 's|github.com/ava-labs/libevm/accounts/abi/bind|github.com/ava-labs/avalanchego/graft/subnet-evm/accounts/abi/bind|g' gen_allowlist_binding.go gen_allowlisttest_binding.go && rm -f gen_allowlist_binding.go.bak gen_allowlisttest_binding.go.bak"
