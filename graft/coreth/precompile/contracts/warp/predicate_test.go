// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/params/extras/extrastest"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/precompiletest"
	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"

	agoUtils "github.com/ava-labs/avalanchego/utils"
	safemath "github.com/ava-labs/avalanchego/utils/math"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

var (
	_ agoUtils.Sortable[*testValidator] = (*testValidator)(nil)

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

	graniteRules = extrastest.ForkToAvalancheRules(upgradetest.Granite)
)

func init() {
	testVdrs = make([]*testValidator, 0, numTestVdrs)
	for i := 0; i < numTestVdrs; i++ {
		testVdrs = append(testVdrs, newTestValidator())
	}
	agoUtils.Sort(testVdrs)

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

func (g GasConfig) GetVerifiedWarpMessageCost(chunks int) uint64 {
	return g.GetVerifiedWarpMessageBase + uint64(chunks)*g.PerWarpMessageChunk
}

func (g GasConfig) SendWarpMessageCost(bytes int) uint64 {
	return g.SendWarpMessageBase + uint64(bytes)*g.PerWarpMessageByte
}

func (g GasConfig) PredicateGasCost(chunks int, signers int) uint64 {
	return g.VerifyPredicateBase + uint64(chunks)*g.PerWarpMessageChunk + uint64(signers)*g.PerWarpSigner
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
func createPredicate(numKeys int) predicate.Predicate {
	warpMsg := createWarpMessage(numKeys)
	return predicate.New(warpMsg.Bytes())
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
		GetWarpValidatorSetF: func(context.Context, uint64, ids.ID) (validators.WarpSet, error) {
			return warpValidators, warpValidatorsErr
		},
	}
	return snowCtx
}

func createValidPredicateTest(
	snowCtx *snow.Context,
	numKeys int,
	predicate predicate.Predicate,
	rules extras.AvalancheRules,
) precompiletest.PredicateTest {
	gasCost := CurrentGasConfig(rules)
	return precompiletest.PredicateTest{
		Config: NewDefaultConfig(utils.NewUint64(0)),
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

func TestWarpMessageFromPrimaryNetwork(t *testing.T) {
	for _, requirePrimaryNetworkSigners := range []bool{true, false} {
		testWarpMessageFromPrimaryNetwork(t, requirePrimaryNetworkSigners)
	}
}

func testWarpMessageFromPrimaryNetwork(t *testing.T, requirePrimaryNetworkSigners bool) {
	require := require.New(t)
	numKeys := 10
	cChainID := ids.GenerateTestID()
	addressedCall, err := payload.NewAddressedCall(agoUtils.RandomBytes(20), agoUtils.RandomBytes(100))
	require.NoError(err)
	unsignedMsg, err := avalancheWarp.NewUnsignedMessage(constants.UnitTestID, cChainID, addressedCall.Bytes())
	require.NoError(err)

	var (
		warpValidators = validators.WarpSet{
			Validators:  make([]*validators.Warp, 0, numKeys),
			TotalWeight: 20 * uint64(numKeys),
		}
		blsSignatures = make([]*bls.Signature, 0, numKeys)
	)
	for i := 0; i < numKeys; i++ {
		vdr := testVdrs[i]
		sig, err := vdr.sk.Sign(unsignedMsg.Bytes())
		require.NoError(err)
		blsSignatures = append(blsSignatures, sig)

		pk := vdr.sk.PublicKey()
		warpValidators.Validators = append(warpValidators.Validators, &validators.Warp{
			PublicKey:      pk,
			PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk),
			Weight:         20,
			NodeIDs:        []ids.NodeID{vdr.nodeID},
		})
	}
	agoUtils.Sort(warpValidators.Validators)

	aggregateSignature, err := bls.AggregateSignatures(blsSignatures)
	require.NoError(err)
	bitSet := set.NewBits()
	for i := 0; i < numKeys; i++ {
		bitSet.Add(i)
	}
	warpSignature := &avalancheWarp.BitSetSignature{
		Signers: bitSet.Bytes(),
	}
	copy(warpSignature.Signature[:], bls.SignatureToBytes(aggregateSignature))
	warpMsg, err := avalancheWarp.NewMessage(unsignedMsg, warpSignature)
	require.NoError(err)

	pred := predicate.New(warpMsg.Bytes())

	snowCtx := snowtest.Context(t, ids.GenerateTestID())
	snowCtx.SubnetID = ids.GenerateTestID()
	snowCtx.CChainID = cChainID
	snowCtx.ValidatorState = &validatorstest.State{
		GetSubnetIDF: func(_ context.Context, chainID ids.ID) (ids.ID, error) {
			require.Equal(chainID, cChainID)
			return constants.PrimaryNetworkID, nil // Return Primary Network SubnetID
		},
		GetWarpValidatorSetF: func(_ context.Context, _ uint64, subnetID ids.ID) (validators.WarpSet, error) {
			expectedSubnetID := snowCtx.SubnetID
			if requirePrimaryNetworkSigners {
				expectedSubnetID = constants.PrimaryNetworkID
			}
			require.Equal(expectedSubnetID, subnetID)
			return warpValidators, nil
		},
	}

	test := precompiletest.PredicateTest{
		Config: NewConfig(utils.NewUint64(0), 0, requirePrimaryNetworkSigners),
		PredicateContext: &precompileconfig.PredicateContext{
			SnowCtx: snowCtx,
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		Predicate:   pred,
		Rules:       graniteRules,
		Gas:         graniteGasConfig.PredicateGasCost(len(pred), numKeys),
		GasErr:      nil,
		ExpectedErr: nil,
	}

	test.Run(t)
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
		Config: NewDefaultConfig(utils.NewUint64(0)),
		PredicateContext: &precompileconfig.PredicateContext{
			SnowCtx: snowCtx,
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		Predicate: pred,
		Rules:     graniteRules,
		Gas:       graniteGasConfig.PredicateGasCost(len(pred), numKeys),
		GasErr:    errInvalidPredicateBytes,
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
	pred := predicate.New(warpMsgBytes)

	test := precompiletest.PredicateTest{
		Config: NewDefaultConfig(utils.NewUint64(0)),
		PredicateContext: &precompileconfig.PredicateContext{
			SnowCtx: snowCtx,
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		Predicate: pred,
		Rules:     graniteRules,
		Gas:       graniteGasConfig.PredicateGasCost(len(pred), numKeys),
		GasErr:    errInvalidWarpMsg,
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
	pred := predicate.New(warpMsgBytes)

	test := precompiletest.PredicateTest{
		Config: NewDefaultConfig(utils.NewUint64(0)),
		PredicateContext: &precompileconfig.PredicateContext{
			SnowCtx: snowCtx,
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		Predicate: pred,
		Rules:     graniteRules,
		Gas:       graniteGasConfig.PredicateGasCost(len(pred), numKeys),
		GasErr:    errInvalidWarpMsgPayload,
	}

	test.Run(t)
}

func TestInvalidBitSet(t *testing.T) {
	addressedCall, err := payload.NewAddressedCall(agoUtils.RandomBytes(20), agoUtils.RandomBytes(100))
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
	pred := predicate.New(msg.Bytes())
	test := precompiletest.PredicateTest{
		Config: NewDefaultConfig(utils.NewUint64(0)),
		PredicateContext: &precompileconfig.PredicateContext{
			SnowCtx: snowCtx,
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		Predicate: pred,
		Rules:     graniteRules,
		Gas:       graniteGasConfig.PredicateGasCost(len(pred), numKeys),
		GasErr:    errCannotGetNumSigners,
	}

	test.Run(t)
}

func TestWarpSignatureWeightsDefaultQuorumNumerator(t *testing.T) {
	snowCtx := createSnowCtx(t, []validatorRange{
		{
			start:     0,
			end:       100,
			weight:    20,
			publicKey: true,
		},
	})

	allNumSigners := []int{
		1,
		int(WarpDefaultQuorumNumerator) - 1,
		int(WarpDefaultQuorumNumerator),
		int(WarpDefaultQuorumNumerator) + 1,
		int(WarpQuorumDenominator) - 1,
		int(WarpQuorumDenominator),
		int(WarpQuorumDenominator) + 1,
	}
	tests := make([]precompiletest.PredicateTest, len(allNumSigners))
	for i, numSigners := range allNumSigners {
		pred := createPredicate(numSigners)
		// The predicate is valid iff the number of signers is >= the required numerator and does not exceed the denominator.
		var expectedErr error
		if numSigners >= int(WarpDefaultQuorumNumerator) && numSigners <= int(WarpQuorumDenominator) {
			expectedErr = nil
		} else {
			expectedErr = errFailedVerification
		}

		tests[i] = precompiletest.PredicateTest{
			Name:   fmt.Sprintf("default quorum %d signature(s)", numSigners),
			Config: NewDefaultConfig(utils.NewUint64(0)),
			PredicateContext: &precompileconfig.PredicateContext{
				SnowCtx: snowCtx,
				ProposerVMBlockCtx: &block.Context{
					PChainHeight: 1,
				},
			},
			Predicate:   pred,
			Rules:       graniteRules,
			Gas:         graniteGasConfig.PredicateGasCost(len(pred), numSigners),
			GasErr:      nil,
			ExpectedErr: expectedErr,
		}
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
				pred        predicate.Predicate
				expectedGas uint64
				expectedErr error
			)
			if valid {
				pred = validPredicate
				expectedGas = graniteGasConfig.PredicateGasCost(len(pred), numSigners)
				expectedErr = nil
			} else {
				pred = invalidPredicate
				expectedGas = graniteGasConfig.PredicateGasCost(len(invalidPredicate), 1)
				expectedErr = errFailedVerification
			}

			tests = append(tests, precompiletest.PredicateTest{
				Name:   fmt.Sprintf("multiple predicates %v", validMessageIndices),
				Config: NewDefaultConfig(utils.NewUint64(0)),
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

func TestWarpSignatureWeightsNonDefaultQuorumNumerator(t *testing.T) {
	snowCtx := createSnowCtx(t, []validatorRange{
		{
			start:     0,
			end:       100,
			weight:    20,
			publicKey: true,
		},
	})

	nonDefaultQuorumNumerator := 50
	// Ensure this test fails if the DefaultQuorumNumerator is changed to an
	// unexpected value during development
	require.NotEqual(t, nonDefaultQuorumNumerator, int(WarpDefaultQuorumNumerator))
	allNumSigners := []int{
		nonDefaultQuorumNumerator,
		nonDefaultQuorumNumerator + 1,
		99,
		100,
		101,
	}
	tests := make([]precompiletest.PredicateTest, len(allNumSigners))
	for i, numSigners := range allNumSigners {
		pred := createPredicate(numSigners)
		// The predicate is valid iff the number of signers is >= the required numerator and does not exceed the denominator.
		var expectedErr error
		if numSigners >= nonDefaultQuorumNumerator && numSigners <= int(WarpQuorumDenominator) {
			expectedErr = nil
		} else {
			expectedErr = errFailedVerification
		}

		tests[i] = precompiletest.PredicateTest{
			Name:   fmt.Sprintf("non-default quorum %d signature(s)", numSigners),
			Config: NewConfig(utils.NewUint64(0), uint64(nonDefaultQuorumNumerator), false),
			PredicateContext: &precompileconfig.PredicateContext{
				SnowCtx: snowCtx,
				ProposerVMBlockCtx: &block.Context{
					PChainHeight: 1,
				},
			},
			Predicate:   pred,
			Rules:       graniteRules,
			Gas:         graniteGasConfig.PredicateGasCost(len(pred), numSigners),
			GasErr:      nil,
			ExpectedErr: expectedErr,
		}
	}

	precompiletest.RunPredicateTests(t, tests)
}

func TestWarpNoValidatorsAndOverflowUseSameGas(t *testing.T) {
	var (
		config            = NewConfig(utils.NewUint64(0), 0, false)
		proposervmContext = &block.Context{
			PChainHeight: 1,
		}
		pred        = createPredicate(0)
		expectedGas = graniteGasConfig.PredicateGasCost(len(pred), 0)
	)
	noValidators := precompiletest.PredicateTest{
		Name:   "no_validators",
		Config: config,
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
		Name:   "weight_overflow",
		Config: config,
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

func makeWarpPredicateTests(tb testing.TB, rules extras.AvalancheRules) []precompiletest.PredicateTest {
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
			GetWarpValidatorSetF: func(context.Context, uint64, ids.ID) (validators.WarpSet, error) {
				return warpValidators, nil
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
			rules := extrastest.ForkToAvalancheRules(fork)
			tests := makeWarpPredicateTests(t, rules)
			precompiletest.RunPredicateTests(t, tests)
		})
	}
}

func BenchmarkWarpPredicate(b *testing.B) {
	for _, fork := range forks {
		b.Run(fork.String(), func(b *testing.B) {
			rules := extrastest.ForkToAvalancheRules(fork)
			tests := makeWarpPredicateTests(b, rules)
			precompiletest.RunPredicateBenchmarks(b, tests)
		})
	}
}
