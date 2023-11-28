// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/precompile/precompileconfig"
	"github.com/ethereum/go-ethereum/common"
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
	predicateResultBytes1 := []byte{1, 2, 3}
	predicateResultBytes2 := []byte{3, 2, 1}
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
				predicater.EXPECT().VerifyPredicate(gomock.Any(), [][]byte{arg[:]}).Return(predicateResultBytes1)
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
				addr1: predicateResultBytes1,
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
				predicater.EXPECT().VerifyPredicate(gomock.Any(), [][]byte{arg[:]}).Return(predicateResultBytes1)
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
				addr1: predicateResultBytes1,
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
				predicate1.EXPECT().VerifyPredicate(gomock.Any(), [][]byte{arg1[:]}).Return(predicateResultBytes1)
				predicate2 := precompileconfig.NewMockPredicater(ctrl)
				arg2 := common.Hash{2}
				predicate2.EXPECT().PredicateGas(arg2[:]).Return(uint64(0), nil).Times(2)
				predicate2.EXPECT().VerifyPredicate(gomock.Any(), [][]byte{arg2[:]}).Return(predicateResultBytes2)
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
				addr1: predicateResultBytes1,
				addr2: predicateResultBytes2,
			},
			expectedErr: nil,
		},
		"two predicates niether named by access list": {
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
		test := test
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			// Create the rules from TestChainConfig and update the predicates based on the test params
			rules := params.TestChainConfig.AvalancheRules(common.Big0, 0)
			if test.createPredicates != nil {
				for address, predicater := range test.createPredicates(t) {
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
