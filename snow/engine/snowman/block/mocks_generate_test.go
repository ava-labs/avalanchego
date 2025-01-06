// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

//go:generate go run go.uber.org/mock/mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/build_block_with_context_chain_vm.go -mock_names=BuildBlockWithContextChainVM=BuildBlockWithContextChainVM . BuildBlockWithContextChainVM
//go:generate go run go.uber.org/mock/mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/chain_vm.go -mock_names=ChainVM=ChainVM . ChainVM
//go:generate go run go.uber.org/mock/mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/state_syncable_vm.go -mock_names=StateSyncableVM=StateSyncableVM . StateSyncableVM
//go:generate go run go.uber.org/mock/mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/with_verify_context.go -mock_names=WithVerifyContext=WithVerifyContext . WithVerifyContext
