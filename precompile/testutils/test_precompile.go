// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testutils

import (
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/subnet-evm/commontype"
	"github.com/ava-labs/subnet-evm/precompile/contract"
	"github.com/ava-labs/subnet-evm/precompile/modules"
	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

// PrecompileTest is a test case for a precompile
type PrecompileTest struct {
	// Caller is the address of the precompile caller
	Caller common.Address
	// Input the raw input bytes to the precompile
	Input []byte
	// InputFn is a function that returns the raw input bytes to the precompile
	// If specified, Input will be ignored.
	InputFn func(t *testing.T) []byte
	// SuppliedGas is the amount of gas supplied to the precompile
	SuppliedGas uint64
	// ReadOnly is whether the precompile should be called in read only
	// mode. If true, the precompile should not modify the state.
	ReadOnly bool
	// Config is the config to use for the precompile
	// It should be the same precompile config that is used in the
	// precompile's configurator.
	// If nil, Configure will not be called.
	Config precompileconfig.Config
	// BeforeHook is called before the precompile is called.
	BeforeHook func(t *testing.T, state contract.StateDB)
	// AfterHook is called after the precompile is called.
	AfterHook func(t *testing.T, state contract.StateDB)
	// ExpectedRes is the expected raw byte result returned by the precompile
	ExpectedRes []byte
	// ExpectedErr is the expected error returned by the precompile
	ExpectedErr string
	// BlockNumber is the block number to use for the precompile's block context
	BlockNumber int64
}

func (test PrecompileTest) Run(t *testing.T, module modules.Module, state contract.StateDB) {
	t.Helper()
	contractAddress := module.Address

	if test.BeforeHook != nil {
		test.BeforeHook(t, state)
	}

	blockContext := contract.NewMockBlockContext(big.NewInt(test.BlockNumber), 0)
	accesibleState := contract.NewMockAccessibleState(state, blockContext, snow.DefaultContextTest())
	chainConfig := contract.NewMockChainState(commontype.ValidTestFeeConfig, false)

	if test.Config != nil {
		err := module.Configure(chainConfig, test.Config, state, blockContext)
		require.NoError(t, err)
	}

	input := test.Input
	if test.InputFn != nil {
		input = test.InputFn(t)
	}

	if input != nil {
		ret, remainingGas, err := module.Contract.Run(accesibleState, test.Caller, contractAddress, input, test.SuppliedGas, test.ReadOnly)
		if len(test.ExpectedErr) != 0 {
			require.ErrorContains(t, err, test.ExpectedErr)
		} else {
			require.NoError(t, err)
		}
		require.Equal(t, uint64(0), remainingGas)
		require.Equal(t, test.ExpectedRes, ret)
	}

	if test.AfterHook != nil {
		test.AfterHook(t, state)
	}
}
