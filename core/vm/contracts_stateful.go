// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"github.com/ava-labs/subnet-evm/precompile"
	"github.com/ethereum/go-ethereum/common"
)

// wrappedPrecompiledContract implements StatefulPrecompiledContract by wrapping stateless native precompiled contracts
// in Ethereum.
type wrappedPrecompiledContract struct {
	p PrecompiledContract
}

// newWrappedPrecompiledContract returns a wrapped version of [PrecompiledContract] to be executed according to the StatefulPrecompiledContract
// interface.
func newWrappedPrecompiledContract(p PrecompiledContract) precompile.StatefulPrecompiledContract {
	return &wrappedPrecompiledContract{p: p}
}

// Run implements the StatefulPrecompiledContract interface
func (w *wrappedPrecompiledContract) Run(accessibleState precompile.PrecompileAccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	return RunPrecompiledContract(w.p, input, suppliedGas)
}

// RequiredGas returns the amount of gas consumed by [w] when operating on [input].
func (w *wrappedPrecompiledContract) RequiredGas(input []byte) uint64 {
	return w.p.RequiredGas(input)
}

// RunStatefulPrecompiledContract confirms runs [precompile] with the specified parameters.
func RunStatefulPrecompiledContract(precompile precompile.StatefulPrecompiledContract, accessibleState precompile.PrecompileAccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	// Confirm that there is sufficient gas to execute [precompile]
	// Note: we perform this check here to so that we can return ErrOutOfGas if [precompile] will run out of gas.
	requiredGas := precompile.RequiredGas(input)
	if requiredGas > suppliedGas {
		return nil, 0, ErrOutOfGas
	}

	return precompile.Run(accessibleState, caller, addr, input, suppliedGas, readOnly)
}
