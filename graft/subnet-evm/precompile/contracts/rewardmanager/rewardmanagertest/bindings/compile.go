// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bindings

// Step 1: Compile interface to generate ABI at top level
//go:generate sh -c "solc-v0.8.30 -o ../.. --overwrite --abi --base-path ../../../../.. --pretty-json --evm-version cancun ../../IRewardManager.sol"
// Step 2: Compile test contracts to generate ABI and bin files
//go:generate solc-v0.8.30 -o artifacts --overwrite --abi --bin --base-path ../../../../.. --metadata-hash none --evm-version cancun RewardManagerTest.sol
// Step 3: Generate Go bindings from the compiled artifacts
//go:generate go run github.com/ava-labs/libevm/cmd/abigen --pkg bindings --type IRewardManager --abi ../../IRewardManager.abi --bin artifacts/IRewardManager.bin --out gen_irewardmanager_binding.go
//go:generate go run github.com/ava-labs/libevm/cmd/abigen --pkg bindings --type RewardManagerTest --abi artifacts/RewardManagerTest.abi --bin artifacts/RewardManagerTest.bin --out gen_rewardmanagertest_binding.go
// Step 4: Replace import paths in generated binding to use subnet-evm instead of libevm
// This is necessary because the libevm bindings package is not compatible with the subnet-evm simulated backend, which is used for testing.
//go:generate sh -c "sed -i.bak -e 's|github.com/ava-labs/libevm/accounts/abi|github.com/ava-labs/avalanchego/graft/subnet-evm/accounts/abi|g' -e 's|github.com/ava-labs/libevm/accounts/abi/bind|github.com/ava-labs/avalanchego/graft/subnet-evm/accounts/abi/bind|g' gen_irewardmanager_binding.go gen_rewardmanagertest_binding.go && rm -f gen_irewardmanager_binding.go.bak gen_rewardmanagertest_binding.go.bak"
