// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type predicateCheckTest struct {
	accessList       types.AccessList
	gas              uint64
	predicateContext *precompileconfig.PredicateContext
	createPredicates func(t testing.TB) map[common.Address]precompileconfig.Predicater
	expectedRes      map[common.Address][]byte
	expectedErr      error
}

func TestCheckPredicate(t *testing.T) {
	testErr := errors.New("test error")
	addr1 := common.HexToAddress("0xaa")
	addr2 := common.HexToAddress("0xbb")
	addr3 := common.HexToAddress("0xcc")
	addr4 := common.HexToAddress("0xdd")
	predicateContext := &precompileconfig.PredicateContext{
		ProposerVMBlockCtx: &block.Context{
			PChainHeight: 10,
		},
	}
	for name, test := range map[string]predicateCheckTest{
		"no predicates, no access list, no context passes": {
			gas:              53000,
			predicateContext: nil,
			expectedRes:      make(map[common.Address][]byte),
			expectedErr:      nil,
		},
		"no predicates, no access list, with context passes": {
			gas:              53000,
			predicateContext: predicateContext,
			expectedRes:      make(map[common.Address][]byte),
			expectedErr:      nil,
		},
		"no predicates, with access list, no context passes": {
			gas:              57300,
			predicateContext: nil,
			accessList: types.AccessList([]types.AccessTuple{
				{
					Address: addr1,
					StorageKeys: []common.Hash{
						{1},
					},
				},
			}),
			expectedRes: make(map[common.Address][]byte),
			expectedErr: nil,
		},
		"predicate, no access list, no context passes": {
			gas:              53000,
			predicateContext: nil,
			createPredicates: func(t testing.TB) map[common.Address]precompileconfig.Predicater {
				predicater := precompileconfig.NewMockPredicater(gomock.NewController(t))
				return map[common.Address]precompileconfig.Predicater{
					addr1: predicater,
				}
			},
			expectedRes: make(map[common.Address][]byte),
			expectedErr: nil,
		},
		"predicate, no access list, no block context passes": {
			gas: 53000,
			predicateContext: &precompileconfig.PredicateContext{
				ProposerVMBlockCtx: nil,
			},
			createPredicates: func(t testing.TB) map[common.Address]precompileconfig.Predicater {
				predicater := precompileconfig.NewMockPredicater(gomock.NewController(t))
				return map[common.Address]precompileconfig.Predicater{
					addr1: predicater,
				}
			},
			expectedRes: make(map[common.Address][]byte),
			expectedErr: nil,
		},
		"predicate named by access list, without context errors": {
			gas:              53000,
			predicateContext: nil,
			createPredicates: func(t testing.TB) map[common.Address]precompileconfig.Predicater {
				predicater := precompileconfig.NewMockPredicater(gomock.NewController(t))
				arg := common.Hash{1}
				predicater.EXPECT().PredicateGas(arg[:]).Return(uint64(0), nil).Times(1)
				return map[common.Address]precompileconfig.Predicater{
					addr1: predicater,
				}
			},
			accessList: types.AccessList([]types.AccessTuple{
				{
					Address: addr1,
					StorageKeys: []common.Hash{
						{1},
					},
				},
			}),
			expectedErr: ErrMissingPredicateContext,
		},
		"predicate named by access list, without block context errors": {
			gas: 53000,
			predicateContext: &precompileconfig.PredicateContext{
				ProposerVMBlockCtx: nil,
			},
			createPredicates: func(t testing.TB) map[common.Address]precompileconfig.Predicater {
				predicater := precompileconfig.NewMockPredicater(gomock.NewController(t))
				arg := common.Hash{1}
				predicater.EXPECT().PredicateGas(arg[:]).Return(uint64(0), nil).Times(1)
				return map[common.Address]precompileconfig.Predicater{
					addr1: predicater,
				}
			},
			accessList: types.AccessList([]types.AccessTuple{
				{
					Address: addr1,
					StorageKeys: []common.Hash{
						{1},
					},
				},
			}),
			expectedErr: ErrMissingPredicateContext,
		},
		"predicate named by access list returns non-empty": {
			gas:              53000,
			predicateContext: predicateContext,
			createPredicates: func(t testing.TB) map[common.Address]precompileconfig.Predicater {
				predicater := precompileconfig.NewMockPredicater(gomock.NewController(t))
				arg := common.Hash{1}
				predicater.EXPECT().PredicateGas(arg[:]).Return(uint64(0), nil).Times(2)
				predicater.EXPECT().VerifyPredicate(gomock.Any(), arg[:]).Return(nil)
				return map[common.Address]precompileconfig.Predicater{
					addr1: predicater,
				}
			},
			accessList: types.AccessList([]types.AccessTuple{
				{
					Address: addr1,
					StorageKeys: []common.Hash{
						{1},
					},
				},
			}),
			expectedRes: map[common.Address][]byte{
				addr1: {}, // valid bytes
			},
			expectedErr: nil,
		},
		"predicate returns gas err": {
			gas:              53000,
			predicateContext: predicateContext,
			createPredicates: func(t testing.TB) map[common.Address]precompileconfig.Predicater {
				predicater := precompileconfig.NewMockPredicater(gomock.NewController(t))
				arg := common.Hash{1}
				predicater.EXPECT().PredicateGas(arg[:]).Return(uint64(0), testErr)
				return map[common.Address]precompileconfig.Predicater{
					addr1: predicater,
				}
			},
			accessList: types.AccessList([]types.AccessTuple{
				{
					Address: addr1,
					StorageKeys: []common.Hash{
						{1},
					},
				},
			}),
			expectedErr: testErr,
		},
		"two predicates one named by access list returns non-empty": {
			gas:              53000,
			predicateContext: predicateContext,
			createPredicates: func(t testing.TB) map[common.Address]precompileconfig.Predicater {
				predicater := precompileconfig.NewMockPredicater(gomock.NewController(t))
				arg := common.Hash{1}
				predicater.EXPECT().PredicateGas(arg[:]).Return(uint64(0), nil).Times(2)
				predicater.EXPECT().VerifyPredicate(gomock.Any(), arg[:]).Return(nil)
				return map[common.Address]precompileconfig.Predicater{
					addr1: predicater,
					addr2: predicater,
				}
			},
			accessList: types.AccessList([]types.AccessTuple{
				{
					Address: addr1,
					StorageKeys: []common.Hash{
						{1},
					},
				},
			}),
			expectedRes: map[common.Address][]byte{
				addr1: {}, // valid bytes
			},
			expectedErr: nil,
		},
		"two predicates both named by access list returns non-empty": {
			gas:              53000,
			predicateContext: predicateContext,
			createPredicates: func(t testing.TB) map[common.Address]precompileconfig.Predicater {
				ctrl := gomock.NewController(t)
				predicate1 := precompileconfig.NewMockPredicater(ctrl)
				arg1 := common.Hash{1}
				predicate1.EXPECT().PredicateGas(arg1[:]).Return(uint64(0), nil).Times(2)
				predicate1.EXPECT().VerifyPredicate(gomock.Any(), arg1[:]).Return(nil)
				predicate2 := precompileconfig.NewMockPredicater(ctrl)
				arg2 := common.Hash{2}
				predicate2.EXPECT().PredicateGas(arg2[:]).Return(uint64(0), nil).Times(2)
				predicate2.EXPECT().VerifyPredicate(gomock.Any(), arg2[:]).Return(testErr)
				return map[common.Address]precompileconfig.Predicater{
					addr1: predicate1,
					addr2: predicate2,
				}
			},
			accessList: types.AccessList([]types.AccessTuple{
				{
					Address: addr1,
					StorageKeys: []common.Hash{
						{1},
					},
				},
				{
					Address: addr2,
					StorageKeys: []common.Hash{
						{2},
					},
				},
			}),
			expectedRes: map[common.Address][]byte{
				addr1: {},  // valid bytes
				addr2: {1}, // invalid bytes
			},
			expectedErr: nil,
		},
		"two predicates neither named by access list": {
			gas:              61600,
			predicateContext: predicateContext,
			createPredicates: func(t testing.TB) map[common.Address]precompileconfig.Predicater {
				predicater := precompileconfig.NewMockPredicater(gomock.NewController(t))
				return map[common.Address]precompileconfig.Predicater{
					addr1: predicater,
					addr2: predicater,
				}
			},
			accessList: types.AccessList([]types.AccessTuple{
				{
					Address: addr3,
					StorageKeys: []common.Hash{
						{1},
					},
				},
				{
					Address: addr4,
					StorageKeys: []common.Hash{
						{1},
					},
				},
			}),
			expectedRes: make(map[common.Address][]byte),
			expectedErr: nil,
		},
		"insufficient gas": {
			gas:              53000,
			predicateContext: predicateContext,
			createPredicates: func(t testing.TB) map[common.Address]precompileconfig.Predicater {
				predicater := precompileconfig.NewMockPredicater(gomock.NewController(t))
				arg := common.Hash{1}
				predicater.EXPECT().PredicateGas(arg[:]).Return(uint64(1), nil)
				return map[common.Address]precompileconfig.Predicater{
					addr1: predicater,
				}
			},
			accessList: types.AccessList([]types.AccessTuple{
				{
					Address: addr1,
					StorageKeys: []common.Hash{
						{1},
					},
				},
			}),
			expectedErr: ErrIntrinsicGas,
		},
	} {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			// Create the rules from TestChainConfig and update the predicates based on the test params
			rules := params.TestChainConfig.Rules(common.Big0, params.IsMergeTODO, 0)
			if test.createPredicates != nil {
				for address, predicater := range test.createPredicates(t) {
					rules := params.GetRulesExtra(rules)
					rules.Predicaters[address] = predicater
				}
			}

			// Specify only the access list, since this test should not depend on any other values
			tx := types.NewTx(&types.DynamicFeeTx{
				AccessList: test.accessList,
				Gas:        test.gas,
			})
			predicateRes, err := CheckPredicates(rules, test.predicateContext, tx)
			require.ErrorIs(err, test.expectedErr)
			if test.expectedErr != nil {
				return
			}
			require.Equal(test.expectedRes, predicateRes)
			intrinsicGas, err := IntrinsicGas(tx.Data(), tx.AccessList(), true, rules)
			require.NoError(err)
			require.Equal(tx.Gas(), intrinsicGas) // Require test specifies exact amount of gas consumed
		})
	}
}

func TestCheckPredicatesOutput(t *testing.T) {
	testErr := errors.New("test error")
	addr1 := common.HexToAddress("0xaa")
	addr2 := common.HexToAddress("0xbb")
	validHash := common.Hash{1}
	invalidHash := common.Hash{2}
	predicateContext := &precompileconfig.PredicateContext{
		ProposerVMBlockCtx: &block.Context{
			PChainHeight: 10,
		},
	}
	type testTuple struct {
		address          common.Address
		isValidPredicate bool
	}
	type resultTest struct {
		name        string
		expectedRes map[common.Address][]byte
		testTuple   []testTuple
	}
	tests := []resultTest{
		{name: "no predicates", expectedRes: map[common.Address][]byte{}},
		{
			name: "one address one predicate",
			testTuple: []testTuple{
				{address: addr1, isValidPredicate: true},
			},
			expectedRes: map[common.Address][]byte{addr1: set.NewBits().Bytes()},
		},
		{
			name: "one address one invalid predicate",
			testTuple: []testTuple{
				{address: addr1, isValidPredicate: false},
			},
			expectedRes: map[common.Address][]byte{addr1: set.NewBits(0).Bytes()},
		},
		{
			name: "one address two invalid predicates",
			testTuple: []testTuple{
				{address: addr1, isValidPredicate: false},
				{address: addr1, isValidPredicate: false},
			},
			expectedRes: map[common.Address][]byte{addr1: set.NewBits(0, 1).Bytes()},
		},
		{
			name: "one address two mixed predicates",
			testTuple: []testTuple{
				{address: addr1, isValidPredicate: true},
				{address: addr1, isValidPredicate: false},
			},
			expectedRes: map[common.Address][]byte{addr1: set.NewBits(1).Bytes()},
		},
		{
			name: "one address mixed predicates",
			testTuple: []testTuple{
				{address: addr1, isValidPredicate: true},
				{address: addr1, isValidPredicate: false},
				{address: addr1, isValidPredicate: false},
				{address: addr1, isValidPredicate: true},
			},
			expectedRes: map[common.Address][]byte{addr1: set.NewBits(1, 2).Bytes()},
		},
		{
			name: "two addresses mixed predicates",
			testTuple: []testTuple{
				{address: addr1, isValidPredicate: true},
				{address: addr2, isValidPredicate: false},
				{address: addr1, isValidPredicate: false},
				{address: addr1, isValidPredicate: false},
				{address: addr2, isValidPredicate: true},
				{address: addr2, isValidPredicate: true},
				{address: addr2, isValidPredicate: false},
				{address: addr2, isValidPredicate: true},
			},
			expectedRes: map[common.Address][]byte{addr1: set.NewBits(1, 2).Bytes(), addr2: set.NewBits(0, 3).Bytes()},
		},
		{
			name: "two addresses all valid predicates",
			testTuple: []testTuple{
				{address: addr1, isValidPredicate: true},
				{address: addr2, isValidPredicate: true},
				{address: addr1, isValidPredicate: true},
				{address: addr1, isValidPredicate: true},
			},
			expectedRes: map[common.Address][]byte{addr1: set.NewBits().Bytes(), addr2: set.NewBits().Bytes()},
		},
		{
			name: "two addresses all invalid predicates",
			testTuple: []testTuple{
				{address: addr1, isValidPredicate: false},
				{address: addr2, isValidPredicate: false},
				{address: addr1, isValidPredicate: false},
				{address: addr1, isValidPredicate: false},
			},
			expectedRes: map[common.Address][]byte{addr1: set.NewBits(0, 1, 2).Bytes(), addr2: set.NewBits(0).Bytes()},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)
			// Create the rules from TestChainConfig and update the predicates based on the test params
			rules := params.TestChainConfig.Rules(common.Big0, params.IsMergeTODO, 0)
			predicater := precompileconfig.NewMockPredicater(gomock.NewController(t))
			predicater.EXPECT().PredicateGas(gomock.Any()).Return(uint64(0), nil).Times(len(test.testTuple))

			var txAccessList types.AccessList
			for _, tuple := range test.testTuple {
				var predicateHash common.Hash
				if tuple.isValidPredicate {
					predicateHash = validHash
					predicater.EXPECT().VerifyPredicate(gomock.Any(), validHash[:]).Return(nil)
				} else {
					predicateHash = invalidHash
					predicater.EXPECT().VerifyPredicate(gomock.Any(), invalidHash[:]).Return(testErr)
				}
				txAccessList = append(txAccessList, types.AccessTuple{
					Address: tuple.address,
					StorageKeys: []common.Hash{
						predicateHash,
					},
				})
			}

			rulesExtra := params.GetRulesExtra(rules)
			rulesExtra.Predicaters[addr1] = predicater
			rulesExtra.Predicaters[addr2] = predicater

			// Specify only the access list, since this test should not depend on any other values
			tx := types.NewTx(&types.DynamicFeeTx{
				AccessList: txAccessList,
				Gas:        53000,
			})
			predicateRes, err := CheckPredicates(rules, predicateContext, tx)
			require.NoError(err)
			require.Equal(test.expectedRes, predicateRes)
		})
	}
}
