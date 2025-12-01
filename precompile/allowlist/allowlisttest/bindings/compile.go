// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bindings

// Step 1: Compile Solidity contracts to generate ABI and bin files
//go:generate solc-v0.8.30 -o artifacts --overwrite --abi --bin --base-path . precompile/=../../ --evm-version cancun AllowListTest.sol
// Step 2: Generate Go bindings from the compiled artifacts
//go:generate go run github.com/ava-labs/libevm/cmd/abigen --pkg bindings --type IAllowList --abi artifacts/IAllowList.abi --bin artifacts/IAllowList.bin --out gen_allowlist_binding.go
//go:generate go run github.com/ava-labs/libevm/cmd/abigen --pkg bindings --type AllowListTest --abi artifacts/AllowListTest.abi --bin artifacts/AllowListTest.bin --out gen_allowlisttest_binding.go
// Step 3: Replace import paths in generated binding to use subnet-evm instead of libevm
//go:generate sh -c "sed -i.bak -e 's|github.com/ava-labs/libevm/accounts/abi|github.com/ava-labs/subnet-evm/accounts/abi|g' -e 's|github.com/ava-labs/libevm/accounts/abi/bind|github.com/ava-labs/subnet-evm/accounts/abi/bind|g' gen_allowlist_binding.go gen_allowlisttest_binding.go && rm -f gen_allowlist_binding.go.bak gen_allowlisttest_binding.go.bak"
