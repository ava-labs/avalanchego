// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

var (
	_ precompileconfig.PrecompilePredicater = (*mockPredicater)(nil)
	_ precompileconfig.ProposerPredicater   = (*mockProposerPredicater)(nil)
)

type mockPredicater struct {
	predicateFunc    func(*precompileconfig.PrecompilePredicateContext, []byte) error
	predicateGasFunc func([]byte) (uint64, error)
}

func (m *mockPredicater) VerifyPredicate(predicateContext *precompileconfig.PrecompilePredicateContext, b []byte) error {
	return m.predicateFunc(predicateContext, b)
}

func (m *mockPredicater) PredicateGas(b []byte) (uint64, error) {
	if m.predicateGasFunc == nil {
		return 0, nil
	}
	return m.predicateGasFunc(b)
}

type mockProposerPredicater struct {
	predicateFunc    func(*precompileconfig.ProposerPredicateContext, []byte) error
	predicateGasFunc func([]byte) (uint64, error)
}

func (m *mockProposerPredicater) VerifyPredicate(predicateContext *precompileconfig.ProposerPredicateContext, b []byte) error {
	return m.predicateFunc(predicateContext, b)
}

func (m *mockProposerPredicater) PredicateGas(b []byte) (uint64, error) {
	if m.predicateGasFunc == nil {
		return 0, nil
	}
	return m.predicateGasFunc(b)
}

type predicateCheckTest struct {
	address               common.Address
	predicater            precompileconfig.PrecompilePredicater
	proposerPredicater    precompileconfig.ProposerPredicater
	accessList            types.AccessList
	gas                   uint64
	emptyProposerBlockCtx bool
	expectedErr           error
}

func TestCheckPredicate(t *testing.T) {
	for name, test := range map[string]predicateCheckTest{
		"no predicates, no access list passes": {
			gas:         53000,
			expectedErr: nil,
		},
		"no predicates, with access list passes": {
			gas: 57300,
			accessList: types.AccessList([]types.AccessTuple{
				{
					Address: common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"),
					StorageKeys: []common.Hash{
						{1},
					},
				},
			}),
			expectedErr: nil,
		},
		"proposer predicate, no access list passes": {
			address:            common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"),
			gas:                53000,
			proposerPredicater: &mockProposerPredicater{predicateFunc: func(*precompileconfig.ProposerPredicateContext, []byte) error { return nil }},
			expectedErr:        nil,
		},
		"predicate, no access list passes": {
			address:     common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"),
			gas:         53000,
			predicater:  &mockPredicater{predicateFunc: func(*precompileconfig.PrecompilePredicateContext, []byte) error { return nil }},
			expectedErr: nil,
		},
		"predicate with valid access list passes": {
			address: common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"),
			gas:     53000,
			predicater: &mockPredicater{
				predicateFunc: func(_ *precompileconfig.PrecompilePredicateContext, b []byte) error {
					if !bytes.Equal(b, common.Hash{1}.Bytes()) {
						return fmt.Errorf("unexpected bytes: 0x%x", b)
					}
					return nil
				},
			},
			accessList: types.AccessList([]types.AccessTuple{
				{
					Address: common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"),
					StorageKeys: []common.Hash{
						{1},
					},
				},
			}),
			expectedErr: nil,
		},
		"predicate with valid access list and non-empty PredicateGas passes": {
			address: common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"),
			gas:     153000,
			predicater: &mockPredicater{
				predicateFunc: func(_ *precompileconfig.PrecompilePredicateContext, b []byte) error {
					if !bytes.Equal(b, common.Hash{1}.Bytes()) {
						return fmt.Errorf("unexpected bytes: 0x%x", b)
					}
					return nil
				},
				predicateGasFunc: func(b []byte) (uint64, error) {
					return 100_000, nil
				},
			},
			accessList: types.AccessList([]types.AccessTuple{
				{
					Address: common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"),
					StorageKeys: []common.Hash{
						{1},
					},
				},
			}),
			expectedErr: nil,
		},
		"proposer predicate with valid access list passes": {
			address: common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"),
			gas:     53000,
			proposerPredicater: &mockProposerPredicater{
				predicateFunc: func(_ *precompileconfig.ProposerPredicateContext, b []byte) error {
					if !bytes.Equal(b, common.Hash{1}.Bytes()) {
						return fmt.Errorf("unexpected bytes: 0x%x", b)
					}
					return nil
				},
			},
			accessList: types.AccessList([]types.AccessTuple{
				{
					Address: common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"),
					StorageKeys: []common.Hash{
						{1},
					},
				},
			}),
			expectedErr: nil,
		},
		"proposer predicate with valid access list and non-empty PredicateGas passes": {
			address: common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"),
			gas:     153000,
			proposerPredicater: &mockProposerPredicater{
				predicateFunc: func(_ *precompileconfig.ProposerPredicateContext, b []byte) error {
					if !bytes.Equal(b, common.Hash{1}.Bytes()) {
						return fmt.Errorf("unexpected bytes: 0x%x", b)
					}
					return nil
				},
				predicateGasFunc: func(b []byte) (uint64, error) {
					return 100_000, nil
				},
			},
			accessList: types.AccessList([]types.AccessTuple{
				{
					Address: common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"),
					StorageKeys: []common.Hash{
						{1},
					},
				},
			}),
			expectedErr: nil,
		},
		"predicate with invalid access list errors": {
			address: common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"),
			gas:     53000,
			predicater: &mockPredicater{
				predicateFunc: func(_ *precompileconfig.PrecompilePredicateContext, b []byte) error {
					if !bytes.Equal(b, common.Hash{1}.Bytes()) {
						return fmt.Errorf("unexpected bytes: 0x%x", b)
					}
					return nil
				},
			},
			accessList: types.AccessList([]types.AccessTuple{
				{
					Address: common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"),
					StorageKeys: []common.Hash{
						{2},
					},
				},
			}),
			expectedErr: fmt.Errorf("unexpected bytes: 0x%x", common.Hash{2}.Bytes()),
		},
		"proposer predicate with invalid access list errors": {
			address: common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"),
			gas:     53000,
			proposerPredicater: &mockProposerPredicater{
				predicateFunc: func(_ *precompileconfig.ProposerPredicateContext, b []byte) error {
					if !bytes.Equal(b, common.Hash{1}.Bytes()) {
						return fmt.Errorf("unexpected bytes: 0x%x", b)
					}
					return nil
				},
			},
			accessList: types.AccessList([]types.AccessTuple{
				{
					Address: common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"),
					StorageKeys: []common.Hash{
						{2},
					},
				},
			}),
			expectedErr: fmt.Errorf("unexpected bytes: 0x%x", common.Hash{2}.Bytes()),
		},
		"proposer predicate with empty proposer block ctx passes": {
			address:               common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"),
			gas:                   53000,
			proposerPredicater:    &mockProposerPredicater{predicateFunc: func(_ *precompileconfig.ProposerPredicateContext, b []byte) error { return nil }},
			emptyProposerBlockCtx: true,
		},
	} {
		test := test
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			// Create the rules from TestChainConfig and update the predicates based on the test params
			rules := params.TestChainConfig.AvalancheRules(common.Big0, 0)
			if test.proposerPredicater != nil {
				rules.ProposerPredicates[test.address] = test.proposerPredicater
			}
			if test.predicater != nil {
				rules.PredicatePrecompiles[test.address] = test.predicater
			}

			// Specify only the access list, since this test should not depend on any other values
			tx := types.NewTx(&types.DynamicFeeTx{
				AccessList: test.accessList,
				Gas:        test.gas,
			})
			predicateContext := &precompileconfig.ProposerPredicateContext{}
			if !test.emptyProposerBlockCtx {
				predicateContext.ProposerVMBlockCtx = &block.Context{}
			}
			err := CheckPredicates(rules, predicateContext, tx)
			if test.expectedErr == nil {
				require.NoError(err)
			} else {
				require.ErrorContains(err, test.expectedErr.Error())
			}
			intrinsicGas, err := IntrinsicGas(tx.Data(), tx.AccessList(), true, rules)
			require.NoError(err)
			require.Equal(tx.Gas(), intrinsicGas) // Require test specifies exact amount of gas consumed
		})
	}
}
