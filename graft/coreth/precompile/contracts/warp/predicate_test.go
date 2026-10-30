// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp_test

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/precompile/precompiletest"
	"github.com/ava-labs/avalanchego/graft/evm/precompileconfig"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/warpmsg"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/cparams"

	safemath "github.com/ava-labs/avalanchego/utils/math"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"

	. "github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
)

var (
	_ utils.Sortable[*testValidator] = (*testValidator)(nil)

	sourceChainID  = ids.GenerateTestID()
	sourceSubnetID = ids.GenerateTestID()

	// valid unsigned warp message used throughout testing
	unsignedMsg *avalancheWarp.UnsignedMessage
	// valid addressed payload
	addressedPayload      *payload.AddressedCall
	addressedPayloadBytes []byte
	// blsSignatures of [unsignedMsg] from each of [testVdrs]
	blsSignatures []*bls.Signature

	numTestVdrs = 10_000
	testVdrs    []*testValidator
	vdrs        map[ids.NodeID]*validators.GetValidatorOutput

	graniteRules = forkToAvalancheRules(upgradetest.Granite)
)

func init() {
	testVdrs = make([]*testValidator, 0, numTestVdrs)
	for i := 0; i < numTestVdrs; i++ {
		testVdrs = append(testVdrs, newTestValidator())
	}
	utils.Sort(testVdrs)

	vdrs = map[ids.NodeID]*validators.GetValidatorOutput{
		testVdrs[0].nodeID: {
			NodeID:    testVdrs[0].nodeID,
			PublicKey: testVdrs[0].vdr.PublicKey,
			Weight:    testVdrs[0].vdr.Weight,
		},
		testVdrs[1].nodeID: {
			NodeID:    testVdrs[1].nodeID,
			PublicKey: testVdrs[1].vdr.PublicKey,
			Weight:    testVdrs[1].vdr.Weight,
		},
		testVdrs[2].nodeID: {
			NodeID:    testVdrs[2].nodeID,
			PublicKey: testVdrs[2].vdr.PublicKey,
			Weight:    testVdrs[2].vdr.Weight,
		},
	}

	var err error
	addr := ids.GenerateTestShortID()
	addressedPayload, err = payload.NewAddressedCall(
		addr[:],
		[]byte{1, 2, 3},
	)
	if err != nil {
		panic(err)
	}
	addressedPayloadBytes = addressedPayload.Bytes()
	unsignedMsg, err = avalancheWarp.NewUnsignedMessage(constants.UnitTestID, sourceChainID, addressedPayload.Bytes())
	if err != nil {
		panic(err)
	}

	for _, testVdr := range testVdrs {
		blsSignature, err := testVdr.sk.Sign(unsignedMsg.Bytes())
		if err != nil {
			panic(err)
		}
		blsSignatures = append(blsSignatures, blsSignature)
	}
}

type testValidator struct {
	nodeID ids.NodeID
	sk     bls.Signer
	vdr    *avalancheWarp.Validator
}

func (v *testValidator) Compare(o *testValidator) int {
	return v.vdr.Compare(o.vdr)
}

func newTestValidator() *testValidator {
	sk, err := localsigner.New()
	if err != nil {
		panic(err)
	}

	nodeID := ids.GenerateTestNodeID()
	pk := sk.PublicKey()
	return &testValidator{
		nodeID: nodeID,
		sk:     sk,
		vdr: &avalancheWarp.Validator{
			PublicKey:      pk,
			PublicKeyBytes: pk.Serialize(),
			Weight:         3,
			NodeIDs:        []ids.NodeID{nodeID},
		},
	}
}

// createWarpMessage constructs a signed warp message using the global variable [unsignedMsg]
// and the first [numKeys] signatures from [blsSignatures]
func createWarpMessage(numKeys int) *avalancheWarp.Message {
	bitSet := set.NewBits()
	for i := 0; i < numKeys; i++ {
		bitSet.Add(i)
	}
	warpSignature := &avalancheWarp.BitSetSignature{
		Signers: bitSet.Bytes(),
	}

	var sig *bls.Signature
	if numKeys > 0 {
		aggregateSignature, err := bls.AggregateSignatures(blsSignatures[0:numKeys])
		if err != nil {
			panic(err)
		}
		sig = aggregateSignature
	} else {
		// Parsing an unpopulated signature fails, so instead we populate a
		// random signature.
		sk, err := localsigner.New()
		if err != nil {
			panic(err)
		}
		sig, err = sk.Sign(unsignedMsg.Bytes())
		if err != nil {
			panic(err)
		}
	}
	copy(warpSignature.Signature[:], bls.SignatureToBytes(sig))

	warpMsg, err := avalancheWarp.NewMessage(unsignedMsg, warpSignature)
	if err != nil {
		panic(err)
	}
	return warpMsg
}

// createPredicate constructs a warp message using createWarpMessage with numKeys signers
// and packs it into predicate encoding.
func createPredicate(numKeys int) warpmsg.Packed {
	warpMsg := createWarpMessage(numKeys)
	return warpmsg.Pack(warpMsg.Bytes())
}

// validatorRange specifies a range of validators to include from [start, end), a staking weight
// to specify for each validator in that range, and whether or not to include the public key.
type validatorRange struct {
	start     int
	end       int
	weight    uint64
	publicKey bool
}

// createSnowCtx creates a snow.Context instance with a validator state specified by the given validatorRanges
func createSnowCtx(tb testing.TB, validatorRanges []validatorRange) *snow.Context {
	validatorSet := make(map[ids.NodeID]*validators.GetValidatorOutput)
	for _, validatorRange := range validatorRanges {
		for i := validatorRange.start; i < validatorRange.end; i++ {
			validatorOutput := &validators.GetValidatorOutput{
				NodeID: testVdrs[i].nodeID,
				Weight: validatorRange.weight,
			}
			if validatorRange.publicKey {
				validatorOutput.PublicKey = testVdrs[i].vdr.PublicKey
			}
			validatorSet[testVdrs[i].nodeID] = validatorOutput
		}
	}

	// Because the warp validator set is pre-calculated after Granite, we avoid
	// flattening in GetWarpValidatorSet to provide more accurate benchmark
	// results.
	warpValidators, warpValidatorsErr := validators.FlattenValidatorSet(validatorSet)

	snowCtx := snowtest.Context(tb, snowtest.CChainID)
	snowCtx.ValidatorState = &validatorstest.State{
		GetSubnetIDF: func(context.Context, ids.ID) (ids.ID, error) {
			return sourceSubnetID, nil
		},
		GetWarpValidatorSetsF: func(context.Context, uint64) (map[ids.ID]validators.WarpSet, error) {
			if warpValidatorsErr != nil {
				return nil, warpValidatorsErr
			}
			return map[ids.ID]validators.WarpSet{
				sourceSubnetID: warpValidators,
			}, nil
		},
	}
	return snowCtx
}

func createValidPredicateTest(
	snowCtx *snow.Context,
	numKeys int,
	predicate warpmsg.Packed,
	rules cparams.Rules,
) precompiletest.PredicateTest {
	gasCost := CurrentGasConfig(rules)
	return precompiletest.PredicateTest{
		PredicateContext: &precompileconfig.PredicateContext{
			SnowCtx: snowCtx,
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		Predicate:   predicate,
		Rules:       rules,
		Gas:         gasCost.PredicateGasCost(len(predicate), numKeys),
		GasErr:      nil,
		ExpectedErr: nil,
	}
}

func TestInvalidPredicatePacking(t *testing.T) {
	numKeys := 1
	snowCtx := createSnowCtx(t, []validatorRange{
		{
			start:     0,
			end:       numKeys,
			weight:    20,
			publicKey: true,
		},
	})
	pred := createPredicate(numKeys)
	pred = append(pred, common.Hash{1}) // Invalidate the predicate byte packing

	test := precompiletest.PredicateTest{
		PredicateContext: &precompileconfig.PredicateContext{
			SnowCtx: snowCtx,
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		Predicate: pred,
		Rules:     graniteRules,
		Gas:       GraniteGasConfig.PredicateGasCost(len(pred), numKeys),
		GasErr:    ErrInvalidPredicateBytes,
	}

	test.Run(t)
}

func TestInvalidWarpMessage(t *testing.T) {
	numKeys := 1
	snowCtx := createSnowCtx(t, []validatorRange{
		{
			start:     0,
			end:       numKeys,
			weight:    20,
			publicKey: true,
		},
	})
	warpMsg := createWarpMessage(1)
	warpMsgBytes := warpMsg.Bytes()
	warpMsgBytes = append(warpMsgBytes, byte(0x01)) // Invalidate warp message packing
	pred := warpmsg.Pack(warpMsgBytes)

	test := precompiletest.PredicateTest{
		PredicateContext: &precompileconfig.PredicateContext{
			SnowCtx: snowCtx,
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		Predicate: pred,
		Rules:     graniteRules,
		Gas:       GraniteGasConfig.PredicateGasCost(len(pred), numKeys),
		GasErr:    ErrInvalidWarpMsg,
	}

	test.Run(t)
}

func TestInvalidAddressedPayload(t *testing.T) {
	numKeys := 1
	snowCtx := createSnowCtx(t, []validatorRange{
		{
			start:     0,
			end:       numKeys,
			weight:    20,
			publicKey: true,
		},
	})
	aggregateSignature, err := bls.AggregateSignatures(blsSignatures[0:numKeys])
	require.NoError(t, err)
	bitSet := set.NewBits()
	for i := 0; i < numKeys; i++ {
		bitSet.Add(i)
	}
	warpSignature := &avalancheWarp.BitSetSignature{
		Signers: bitSet.Bytes(),
	}
	copy(warpSignature.Signature[:], bls.SignatureToBytes(aggregateSignature))
	// Create an unsigned message with an invalid addressed payload
	unsignedMsg, err := avalancheWarp.NewUnsignedMessage(constants.UnitTestID, sourceChainID, []byte{1, 2, 3})
	require.NoError(t, err)
	warpMsg, err := avalancheWarp.NewMessage(unsignedMsg, warpSignature)
	require.NoError(t, err)
	warpMsgBytes := warpMsg.Bytes()
	pred := warpmsg.Pack(warpMsgBytes)

	test := precompiletest.PredicateTest{
		PredicateContext: &precompileconfig.PredicateContext{
			SnowCtx: snowCtx,
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		Predicate: pred,
		Rules:     graniteRules,
		Gas:       GraniteGasConfig.PredicateGasCost(len(pred), numKeys),
		GasErr:    ErrInvalidWarpMsgPayload,
	}

	test.Run(t)
}

func TestInvalidBitSet(t *testing.T) {
	addressedCall, err := payload.NewAddressedCall(utils.RandomBytes(20), utils.RandomBytes(100))
	require.NoError(t, err)
	unsignedMsg, err := avalancheWarp.NewUnsignedMessage(
		constants.UnitTestID,
		sourceChainID,
		addressedCall.Bytes(),
	)
	require.NoError(t, err)

	msg, err := avalancheWarp.NewMessage(
		unsignedMsg,
		&avalancheWarp.BitSetSignature{
			Signers:   make([]byte, 1),
			Signature: [bls.SignatureLen]byte{},
		},
	)
	require.NoError(t, err)

	numKeys := 1
	snowCtx := createSnowCtx(t, []validatorRange{
		{
			start:     0,
			end:       numKeys,
			weight:    20,
			publicKey: true,
		},
	})
	pred := warpmsg.Pack(msg.Bytes())
	test := precompiletest.PredicateTest{
		PredicateContext: &precompileconfig.PredicateContext{
			SnowCtx: snowCtx,
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		Predicate: pred,
		Rules:     graniteRules,
		Gas:       GraniteGasConfig.PredicateGasCost(len(pred), numKeys),
		GasErr:    ErrCannotGetNumSigners,
	}

	test.Run(t)
}

// TestWarpSignatureWeightsQuorumNumerator pins the 67% quorum with 100
// equally weighted validators.
func TestWarpSignatureWeightsQuorumNumerator(t *testing.T) {
	snowCtx := createSnowCtx(t, []validatorRange{
		{
			start:     0,
			end:       100,
			weight:    20,
			publicKey: true,
		},
	})

	var tests []precompiletest.PredicateTest
	for _, tt := range []struct {
		numSigners int
		wantErr    error
	}{
		{numSigners: 1, wantErr: ErrFailedVerification},
		{numSigners: 66, wantErr: ErrFailedVerification},
		{numSigners: 67},
		{numSigners: 68},
		{numSigners: 99},
		{numSigners: 100},
		{numSigners: 101, wantErr: ErrFailedVerification},
	} {
		pred := createPredicate(tt.numSigners)
		tests = append(tests, precompiletest.PredicateTest{
			Name: fmt.Sprintf("%d_signers", tt.numSigners),
			PredicateContext: &precompileconfig.PredicateContext{
				SnowCtx: snowCtx,
				ProposerVMBlockCtx: &block.Context{
					PChainHeight: 1,
				},
			},
			Predicate:   pred,
			Rules:       graniteRules,
			Gas:         GraniteGasConfig.PredicateGasCost(len(pred), tt.numSigners),
			ExpectedErr: tt.wantErr,
		})
	}
	precompiletest.RunPredicateTests(t, tests)
}

// multiple messages all correct, multiple messages all incorrect, mixed bag
func TestWarpMultiplePredicates(t *testing.T) {
	snowCtx := createSnowCtx(t, []validatorRange{
		{
			start:     0,
			end:       100,
			weight:    20,
			publicKey: true,
		},
	})

	var tests []precompiletest.PredicateTest
	for _, validMessageIndices := range [][]bool{
		{},
		{true, false},
		{false, true},
		{false, false},
		{true, true},
	} {
		var (
			numSigners       = int(WarpQuorumDenominator)
			invalidPredicate = createPredicate(1)
			validPredicate   = createPredicate(numSigners)
		)

		for _, valid := range validMessageIndices {
			var (
				pred        warpmsg.Packed
				expectedGas uint64
				expectedErr error
			)
			if valid {
				pred = validPredicate
				expectedGas = GraniteGasConfig.PredicateGasCost(len(pred), numSigners)
				expectedErr = nil
			} else {
				pred = invalidPredicate
				expectedGas = GraniteGasConfig.PredicateGasCost(len(invalidPredicate), 1)
				expectedErr = ErrFailedVerification
			}

			tests = append(tests, precompiletest.PredicateTest{
				Name: fmt.Sprintf("multiple predicates %v", validMessageIndices),
				PredicateContext: &precompileconfig.PredicateContext{
					SnowCtx: snowCtx,
					ProposerVMBlockCtx: &block.Context{
						PChainHeight: 1,
					},
				},
				Predicate:   pred,
				Rules:       graniteRules,
				Gas:         expectedGas,
				GasErr:      nil,
				ExpectedErr: expectedErr,
			})
		}
	}
	precompiletest.RunPredicateTests(t, tests)
}

func TestWarpNoValidatorsAndOverflowUseSameGas(t *testing.T) {
	var (
		proposervmContext = &block.Context{
			PChainHeight: 1,
		}
		pred        = createPredicate(0)
		expectedGas = GraniteGasConfig.PredicateGasCost(len(pred), 0)
	)
	noValidators := precompiletest.PredicateTest{
		Name: "no_validators",
		PredicateContext: &precompileconfig.PredicateContext{
			SnowCtx:            createSnowCtx(t, nil /*=validators*/), // No validators in state
			ProposerVMBlockCtx: proposervmContext,
		},
		Rules:       graniteRules,
		Predicate:   pred,
		Gas:         expectedGas,
		GasErr:      nil,
		ExpectedErr: bls.ErrNoPublicKeys,
	}
	weightOverflow := precompiletest.PredicateTest{
		Name: "weight_overflow",
		PredicateContext: &precompileconfig.PredicateContext{
			SnowCtx: createSnowCtx(t, []validatorRange{
				{
					start:     0,
					end:       2, // Generate two validators each with max weight to force overflow
					weight:    math.MaxUint64,
					publicKey: true,
				},
			}),
			ProposerVMBlockCtx: proposervmContext,
		},
		Predicate:   pred,
		Rules:       graniteRules,
		Gas:         expectedGas,
		GasErr:      nil,
		ExpectedErr: safemath.ErrOverflow,
	}
	precompiletest.RunPredicateTests(t, []precompiletest.PredicateTest{
		noValidators,
		weightOverflow,
	})
}

func makeWarpPredicateTests(tb testing.TB, rules cparams.Rules) []precompiletest.PredicateTest {
	allTotalNodes := []int{10, 100, 1_000, 10_000}
	tests := make([]precompiletest.PredicateTest, len(allTotalNodes))
	for i, totalNodes := range allTotalNodes {
		pred := createPredicate(totalNodes)
		snowCtx := createSnowCtx(tb, []validatorRange{
			{
				start:     0,
				end:       totalNodes,
				weight:    20,
				publicKey: true,
			},
		})
		test := createValidPredicateTest(snowCtx, totalNodes, pred, rules)
		test.Name = fmt.Sprintf("%d signers/%d validators", totalNodes, totalNodes)
		tests[i] = test
	}

	numSigners := 10
	for _, totalNodes := range []int{100, 1_000, 10_000} {
		pred := createPredicate(numSigners)
		snowCtx := createSnowCtx(tb, []validatorRange{
			{
				start:     0,
				end:       numSigners,
				weight:    10_000_000,
				publicKey: true,
			},
			{
				start:     numSigners,
				end:       totalNodes,
				weight:    20,
				publicKey: true,
			},
		})
		test := createValidPredicateTest(snowCtx, numSigners, pred, rules)
		test.Name = fmt.Sprintf("%d signers (heavily weighted)/%d validators", numSigners, totalNodes)
		tests = append(tests, test)
	}

	for _, totalNodes := range []int{100, 1_000, 10_000} {
		pred := createPredicate(numSigners)
		snowCtx := createSnowCtx(tb, []validatorRange{
			{
				start:     0,
				end:       numSigners,
				weight:    10_000_000,
				publicKey: true,
			},
			{
				start:     numSigners,
				end:       totalNodes,
				weight:    20,
				publicKey: false,
			},
		})
		test := createValidPredicateTest(snowCtx, numSigners, pred, rules)
		test.Name = fmt.Sprintf("%d signers (heavily weighted)/%d validators (non-signers without registered PublicKey)", numSigners, totalNodes)
		tests = append(tests, test)
	}

	for _, totalNodes := range []int{100, 1_000, 10_000} {
		pred := createPredicate(numSigners)
		validatorSet := make(map[ids.NodeID]*validators.GetValidatorOutput, totalNodes)
		for i := 0; i < totalNodes; i++ {
			validatorSet[testVdrs[i].nodeID] = &validators.GetValidatorOutput{
				NodeID:    testVdrs[i].nodeID,
				Weight:    20,
				PublicKey: testVdrs[i%numSigners].vdr.PublicKey,
			}
		}
		warpValidators, err := validators.FlattenValidatorSet(validatorSet)
		require.NoError(tb, err)

		snowCtx := snowtest.Context(tb, snowtest.CChainID)
		snowCtx.ValidatorState = &validatorstest.State{
			GetSubnetIDF: func(context.Context, ids.ID) (ids.ID, error) {
				return sourceSubnetID, nil
			},
			GetWarpValidatorSetsF: func(context.Context, uint64) (map[ids.ID]validators.WarpSet, error) {
				return map[ids.ID]validators.WarpSet{
					sourceSubnetID: warpValidators,
				}, nil
			},
		}

		test := createValidPredicateTest(snowCtx, numSigners, pred, rules)
		test.Name = fmt.Sprintf("%d validators w/ %d signers/repeated PublicKeys", totalNodes, numSigners)
		tests = append(tests, test)
	}
	return tests
}

func TestWarpPredicate(t *testing.T) {
	for _, fork := range forks {
		t.Run(fork.String(), func(t *testing.T) {
			rules := forkToAvalancheRules(fork)
			tests := makeWarpPredicateTests(t, rules)
			precompiletest.RunPredicateTests(t, tests)
		})
	}
}

func BenchmarkWarpPredicate(b *testing.B) {
	for _, fork := range forks {
		b.Run(fork.String(), func(b *testing.B) {
			rules := forkToAvalancheRules(fork)
			tests := makeWarpPredicateTests(b, rules)
			precompiletest.RunPredicateBenchmarks(b, tests)
		})
	}
}
