// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/libevm"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/evm/precompile"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/warp"

	evmprecompileconfig "github.com/ava-labs/avalanchego/graft/evm/precompileconfig"
)

var (
	// The native-asset precompiles were deprecated in Banff. They remain
	// precompiles whose every call reverts.
	nativeAssetGenesisAddr = common.HexToAddress("0x0100000000000000000000000000000000000000")
	nativeAssetBalanceAddr = common.HexToAddress("0x0100000000000000000000000000000000000001")
	nativeAssetCallAddr    = common.HexToAddress("0x0100000000000000000000000000000000000002")
	// p256VerifyAddress hosts libevm's P256 signature verification, active
	// since Granite.
	p256VerifyAddress = common.BytesToAddress([]byte{0x1, 0x00})
)

// newPrecompiles returns the precompiles active on the C-Chain under Helicon
// rules, the only rules under which this set is served. The coupling is to
// coreth's rules hooks, which honour the set only when IsHelicon; earlier
// blocks (e.g. replayed through debug RPCs) resolve precompiles on coreth's
// own path instead.
func newPrecompiles(ctx *snow.Context) *precompile.Set {
	return &precompile.Set{
		Contracts: map[common.Address]libevm.PrecompiledContract{
			nativeAssetGenesisAddr: deprecatedPrecompile(),
			nativeAssetBalanceAddr: deprecatedPrecompile(),
			nativeAssetCallAddr:    deprecatedPrecompile(),
			p256VerifyAddress:      &vm.P256Verify{},
			warp.ContractAddress:   warp.NewPrecompile(ctx),
		},
		Predicaters: map[common.Address]evmprecompileconfig.Predicater{
			warp.ContractAddress: warp.Predicater{},
		},
		// coreth pre-warmed only its built-in table and never reported warp
		// as active. This is consensus-visible (EIP-2929), so it is kept.
		Active: []common.Address{
			nativeAssetGenesisAddr,
			nativeAssetBalanceAddr,
			nativeAssetCallAddr,
			p256VerifyAddress,
		},
	}
}

// deprecatedPrecompile reverts every call, refunding unused gas, except for
// DELEGATECALL and CALLCODE which coreth has rejected since Granite by
// consuming all gas.
func deprecatedPrecompile() libevm.PrecompiledContract {
	return vm.NewStatefulPrecompile(func(env vm.PrecompileEnvironment, _ []byte) ([]byte, error) {
		switch env.IncomingCallType() {
		case vm.DelegateCall, vm.CallCode:
			env.UseGas(env.Gas())
		}
		return nil, vm.ErrExecutionReverted
	})
}
