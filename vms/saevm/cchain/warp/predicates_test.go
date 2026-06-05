// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"

	corethwarp "github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

var (
	validPredicate      = predicate.New([]byte{0})
	invalidPredicate    = predicate.New([]byte{1})
	errInvalidPredicate = errors.New("invalid predicate")
)

type predicater struct{}

func (predicater) PredicateGas(predicate.Predicate, precompileconfig.Rules) (uint64, error) {
	return 0, nil
}

func (predicater) VerifyPredicate(_ *precompileconfig.PredicateContext, pred predicate.Predicate) error {
	if slices.Equal(pred, validPredicate) {
		return nil
	}
	return errInvalidPredicate
}

func newRules(contracts ...common.Address) *extras.Rules {
	rules := params.TestChainConfig.Rules(common.Big0, params.IsMergeTODO, 0)
	rulesExtra := params.GetRulesExtra(rules)
	for _, addr := range contracts {
		rulesExtra.Predicaters[addr] = predicater{}
	}
	return rulesExtra
}

func TestBlockPredicates(t *testing.T) {
	var (
		addr0 = common.Address{0}
		addr1 = common.Address{1}

		validTx = types.NewTx(&types.DynamicFeeTx{
			AccessList: types.AccessList{
				{Address: addr0, StorageKeys: validPredicate},
			},
		})
		invalidTx = types.NewTx(&types.DynamicFeeTx{
			AccessList: types.AccessList{
				{Address: addr0, StorageKeys: invalidPredicate},
			},
		})
		twoInvalidTx = types.NewTx(&types.DynamicFeeTx{
			AccessList: types.AccessList{
				{Address: addr0, StorageKeys: invalidPredicate},
				{Address: addr0, StorageKeys: invalidPredicate},
			},
		})
		mixedTx = types.NewTx(&types.DynamicFeeTx{
			AccessList: types.AccessList{
				{Address: addr0, StorageKeys: validPredicate},
				{Address: addr0, StorageKeys: invalidPredicate},
				{Address: addr0, StorageKeys: invalidPredicate},
				{Address: addr0, StorageKeys: validPredicate},
			},
		})
		twoAddressTx = types.NewTx(&types.DynamicFeeTx{
			AccessList: types.AccessList{
				{Address: addr0, StorageKeys: validPredicate},
				{Address: addr1, StorageKeys: invalidPredicate},
				{Address: addr0, StorageKeys: invalidPredicate},
				{Address: addr0, StorageKeys: invalidPredicate},
				{Address: addr1, StorageKeys: validPredicate},
				{Address: addr1, StorageKeys: validPredicate},
				{Address: addr1, StorageKeys: invalidPredicate},
				{Address: addr1, StorageKeys: validPredicate},
			},
		})
	)
	tests := []struct {
		name         string
		contracts    []common.Address
		blockContext *block.Context
		txs          []*types.Transaction
		expected     predicate.BlockResults
		expectedErr  error
	}{
		{
			name:         "no_predicaters",
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				validTx,
			},
		},
		{
			name:         "no_predicates",
			contracts:    []common.Address{addr0},
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				types.NewTx(&types.DynamicFeeTx{}),
			},
		},
		{
			name:         "filtered_predicates",
			contracts:    []common.Address{addr0},
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				types.NewTx(&types.DynamicFeeTx{
					AccessList: types.AccessList{
						{Address: addr1, StorageKeys: validPredicate},
					},
				}),
			},
		},
		{
			name:      "no_block_context",
			contracts: []common.Address{addr0},
			txs: []*types.Transaction{
				validTx,
			},
			expectedErr: errNoBlockContext,
		},
		{
			name:         "one_tx_one_address_one_predicate",
			contracts:    []common.Address{addr0},
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				validTx,
			},
			expected: predicate.BlockResults{
				validTx.Hash(): {
					addr0: set.NewBits(),
				},
			},
		},
		{
			name:         "one_tx_one_address_one_invalid_predicate",
			contracts:    []common.Address{addr0},
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				invalidTx,
			},
			expected: predicate.BlockResults{
				invalidTx.Hash(): {
					addr0: set.NewBits(0),
				},
			},
		},
		{
			name:         "one_address_multiple_invalid_predicates",
			contracts:    []common.Address{addr0},
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				twoInvalidTx,
			},
			expected: predicate.BlockResults{
				twoInvalidTx.Hash(): {
					addr0: set.NewBits(0, 1),
				},
			},
		},
		{
			name:         "one_address_mixed_predicates",
			contracts:    []common.Address{addr0},
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				mixedTx,
			},
			expected: predicate.BlockResults{
				mixedTx.Hash(): {
					addr0: set.NewBits(1, 2),
				},
			},
		},
		{
			name:         "two_addresses_mixed_predicates",
			contracts:    []common.Address{addr0, addr1},
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				twoAddressTx,
			},
			expected: predicate.BlockResults{
				twoAddressTx.Hash(): {
					addr0: set.NewBits(1, 2),
					addr1: set.NewBits(0, 3),
				},
			},
		},
		{
			name:         "multiple_txs",
			contracts:    []common.Address{addr0},
			blockContext: &block.Context{},
			txs: []*types.Transaction{
				validTx,
				invalidTx,
			},
			expected: predicate.BlockResults{
				validTx.Hash(): {
					addr0: set.NewBits(),
				},
				invalidTx.Hash(): {
					addr0: set.NewBits(0),
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var (
				snowContext = snowtest.Context(t, snowtest.CChainID)
				rules       = newRules(test.contracts...)
			)
			actual, err := verifyBlock(snowContext, test.blockContext, rules, test.txs)
			require.ErrorIs(t, err, test.expectedErr)
			require.Equal(t, test.expected, actual)
		})
	}
}

// validatorSet is a set of BLS validators held in canonical (public-key-sorted)
// order, so the signer bitset of a signature it produces lines up with the
// validator set used to verify it.
type validatorSet struct {
	validators []*validators.Warp
	signers    []bls.Signer // parallel to validators
}

// newValidatorSet creates numValidators BLS validators, each with weight 1.
func newValidatorSet(tb testing.TB, numValidators int) validatorSet {
	type keyedValidator struct {
		vdr *validators.Warp
		sk  bls.Signer
	}
	keyed := make([]keyedValidator, numValidators)
	for i := range keyed {
		sk, err := localsigner.New()
		require.NoError(tb, err)
		pk := sk.PublicKey()
		keyed[i] = keyedValidator{
			vdr: &validators.Warp{
				PublicKey:      pk,
				PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk),
				Weight:         1,
				NodeIDs:        []ids.NodeID{ids.GenerateTestNodeID()},
			},
			sk: sk,
		}
	}
	slices.SortFunc(keyed, func(a, b keyedValidator) int {
		return a.vdr.Compare(b.vdr)
	})

	vs := validatorSet{
		validators: make([]*validators.Warp, numValidators),
		signers:    make([]bls.Signer, numValidators),
	}
	for i, kv := range keyed {
		vs.validators[i] = kv.vdr
		vs.signers[i] = kv.sk
	}
	return vs
}

// Sign aggregates a signature over msg from every validator in the set and
// returns the resulting signed warp message.
func (vs validatorSet) Sign(tb testing.TB, msg *avalancheWarp.UnsignedMessage) *avalancheWarp.Message {
	blsSignatures := make([]*bls.Signature, len(vs.signers))
	signerIndices := set.NewBits()
	for i, sk := range vs.signers {
		sig, err := sk.Sign(msg.Bytes())
		require.NoError(tb, err)
		blsSignatures[i] = sig
		signerIndices.Add(i)
	}

	aggregateSignature, err := bls.AggregateSignatures(blsSignatures)
	require.NoError(tb, err)
	warpSignature := &avalancheWarp.BitSetSignature{
		Signers: signerIndices.Bytes(),
	}
	copy(warpSignature.Signature[:], bls.SignatureToBytes(aggregateSignature))

	signedMsg, err := avalancheWarp.NewMessage(msg, warpSignature)
	require.NoError(tb, err)
	return signedMsg
}

// ToWarp returns the validator set in the form expected during verification.
func (vs validatorSet) ToWarp() validators.WarpSet {
	return validators.WarpSet{
		Validators:  vs.validators,
		TotalWeight: uint64(len(vs.validators)),
	}
}

// newUnsigned builds an unsigned warp message from a random source chain.
func newUnsigned(tb testing.TB) *avalancheWarp.UnsignedMessage {
	addressedCall, err := payload.NewAddressedCall([]byte{1, 2, 3}, []byte{4, 5, 6})
	require.NoError(tb, err)
	msg, err := avalancheWarp.NewUnsignedMessage(constants.UnitTestID, ids.GenerateTestID(), addressedCall.Bytes())
	require.NoError(tb, err)
	return msg
}

// warpValidatorState returns a validator state that attributes every chain to
// subnetID and serves warpSet as that subnet's warp validators.
func warpValidatorState(subnetID ids.ID, warpSet validators.WarpSet) *validatorstest.State {
	return &validatorstest.State{
		GetSubnetIDF: func(context.Context, ids.ID) (ids.ID, error) {
			return subnetID, nil
		},
		GetWarpValidatorSetsF: func(context.Context, uint64) (map[ids.ID]validators.WarpSet, error) {
			return map[ids.ID]validators.WarpSet{
				subnetID: warpSet,
			}, nil
		},
	}
}

// newWarpRules returns rules with the production warp precompile registered at
// its contract address.
func newWarpRules() *extras.Rules {
	rules := params.GetRulesExtra(params.TestChainConfig.Rules(common.Big0, params.IsMergeTODO, 0))
	blockTime := uint64(0)
	rules.Predicaters[corethwarp.ContractAddress] = corethwarp.NewDefaultConfig(&blockTime)
	return rules
}

// BenchmarkBlockPredicates measures predicate verification of a block using the
// production warp precompile, so each predicate performs a real BLS aggregate
// signature verification. The matrix varies the block size (txs) and the number
// of predicates per transaction.
//
// It only depends on verifyBlock's stable signature, so the same benchmark
// can be run on the sae-devnet-5 branch and compared with benchstat.
func BenchmarkBlockPredicates(b *testing.B) {
	// The aggregate signature size barely affects verification time, so the
	// number of signers is held fixed.
	const numSigners = 10

	addr := corethwarp.ContractAddress
	for _, numTxs := range []int{1, 10, 100} {
		for _, predicatesPerTx := range []int{1, 10, 100} {
			b.Run(fmt.Sprintf("txs=%d/predicates_per_tx=%d", numTxs, predicatesPerTx), func(b *testing.B) {
				vdrSet := newValidatorSet(b, numSigners)
				snowContext := snowtest.Context(b, snowtest.CChainID)
				sourceSubnetID := ids.GenerateTestID()
				snowContext.ValidatorState = warpValidatorState(sourceSubnetID, vdrSet.ToWarp())

				blockContext := &block.Context{}

				rules := newWarpRules()

				signedMsg := vdrSet.Sign(b, newUnsigned(b))
				pred := predicate.New(signedMsg.Bytes())
				accessList := make(types.AccessList, predicatesPerTx)
				for i := range accessList {
					accessList[i] = types.AccessTuple{
						Address:     addr,
						StorageKeys: pred,
					}
				}

				txs := make([]*types.Transaction, numTxs)
				for i := range txs {
					// A unique nonce gives every transaction a distinct hash,
					// matching the per-tx work of a real block.
					txs[i] = types.NewTx(&types.DynamicFeeTx{
						Nonce:      uint64(i),
						AccessList: accessList,
					})
				}

				// Confirm the predicates verify before timing, so the benchmark
				// measures the success path rather than an early failure.
				results, err := verifyBlock(snowContext, blockContext, rules, txs)
				require.NoError(b, err)
				require.Len(b, results, numTxs)
				require.Equal(b, set.NewBits(), results[txs[0].Hash()][addr])

				for b.Loop() {
					_, _ = verifyBlock(snowContext, blockContext, rules, txs)
				}
			})
		}
	}
}
