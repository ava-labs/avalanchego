// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

//go:generate go tool mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/build_block_with_context_chain_vm.go -mock_names=BuildBlockWithContextChainVM=BuildBlockWithContextChainVM . BuildBlockWithContextChainVM
//go:generate go tool mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/chain_vm.go -mock_names=ChainVM=ChainVM . ChainVM
//go:generate go tool mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/state_syncable_vm.go -mock_names=StateSyncableVM=StateSyncableVM . StateSyncableVM
//go:generate go tool mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/with_verify_context.go -mock_names=WithVerifyContext=WithVerifyContext . WithVerifyContext
//go:generate go tool mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/full_vm.go -mock_names=FullVM=FullVM . FullVM
