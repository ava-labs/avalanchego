// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bindings

// Step 1: Compile interface to generate ABI at top level
//go:generate solc -o ../.. --overwrite --abi --base-path ../../../../.. --pretty-json --evm-version cancun ../../IGasPriceManager.sol
// Step 2: Compile test contract to generate ABI and bin files
//go:generate solc -o artifacts --overwrite --abi --bin --base-path ../../../../.. --metadata-hash none --evm-version cancun GasPriceManagerTest.sol
// Step 3: Generate Go bindings from the compiled artifacts
//go:generate go run github.com/ava-labs/libevm/cmd/abigen --pkg bindings --type IGasPriceManager --abi ../../IGasPriceManager.abi --out gen_igaspricemanager_binding.go
//go:generate go run github.com/ava-labs/libevm/cmd/abigen --pkg bindings --type GasPriceManagerTest --abi artifacts/GasPriceManagerTest.abi --bin artifacts/GasPriceManagerTest.bin --out gen_gaspricemanagertest_binding.go
// Step 4: Remove duplicate IGasPriceManagerGasPriceConfig struct from test binding (already defined in interface binding)
//go:generate sh -c "sed -i.bak '/IGasPriceManagerGasPriceConfig is an auto/,/^}/d' gen_gaspricemanagertest_binding.go && rm -f gen_gaspricemanagertest_binding.go.bak"
// Step 5: Replace import paths in generated binding to use subnet-evm instead of libevm
// This is necessary because the libevm bindings package is not compatible with the subnet-evm simulated backend, which is used for testing.
//go:generate sh -c "sed -i.bak -e 's|github.com/ava-labs/libevm/accounts/abi/bind|github.com/ava-labs/avalanchego/graft/subnet-evm/accounts/abi/bind|g' gen_igaspricemanager_binding.go gen_gaspricemanagertest_binding.go && rm -f gen_igaspricemanager_binding.go.bak gen_gaspricemanagertest_binding.go.bak"
