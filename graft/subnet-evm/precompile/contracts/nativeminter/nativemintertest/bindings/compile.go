// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bindings

// Step 1: Compile interface to generate ABI at top level
//go:generate sh -c "solc-v0.8.30 -o ../.. --overwrite --abi --base-path ../../../../.. --pretty-json --evm-version cancun ../../INativeMinter.sol"
// Step 2: Compile test contracts to generate ABI and bin files
//go:generate solc-v0.8.30 -o artifacts --overwrite --abi --bin --base-path ../../../../.. --metadata-hash none  --evm-version cancun NativeMinterTest.sol
// Step 3: Generate Go bindings from the compiled artifacts
//go:generate go run github.com/ava-labs/libevm/cmd/abigen --pkg bindings --type INativeMinter --abi ../../INativeMinter.abi --bin artifacts/INativeMinter.bin --out gen_inativeminter_binding.go
//go:generate go run github.com/ava-labs/libevm/cmd/abigen --pkg bindings --type NativeMinterTest --abi artifacts/NativeMinterTest.abi --bin artifacts/NativeMinterTest.bin --out gen_nativemintertest_binding.go
// Step 4: Replace import paths in generated binding to use subnet-evm instead of libevm
// This is necessary because the libevm bindings package is not compatible with the subnet-evm simulated backend, which is used for testing.
//go:generate sh -c "sed -i.bak -e 's|github.com/ava-labs/libevm/accounts/abi|github.com/ava-labs/avalanchego/graft/subnet-evm/accounts/abi|g' -e 's|github.com/ava-labs/libevm/accounts/abi/bind|github.com/ava-labs/avalanchego/graft/subnet-evm/accounts/abi/bind|g' gen_inativeminter_binding.go gen_nativemintertest_binding.go && rm -f gen_inativeminter_binding.go.bak gen_nativemintertest_binding.go.bak"
