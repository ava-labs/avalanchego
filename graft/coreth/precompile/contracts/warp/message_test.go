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

// createPacked constructs a warp message using createWarpMessage with numKeys signers
// and packs it with [warpmsg.Pack].
func createPacked(numKeys int) warpmsg.Packed {
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

func createValidMessageTest(
	snowCtx *snow.Context,
	numKeys int,
	packed warpmsg.Packed,
	rules cparams.Rules,
) precompiletest.MessageTest {
	gasCost := CurrentGasConfig(rules)
	return precompiletest.MessageTest{
		VerifyContext: &warpmsg.VerifyContext{
			SnowCtx: snowCtx,
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		Packed:      packed,
		Rules:       rules,
		Gas:         gasCost.MessageGasCost(len(packed), numKeys),
		GasErr:      nil,
		ExpectedErr: nil,
	}
}

func TestInvalidPacking(t *testing.T) {
	numKeys := 1
	snowCtx := createSnowCtx(t, []validatorRange{
		{
			start:     0,
			end:       numKeys,
			weight:    20,
			publicKey: true,
		},
	})
	packed := createPacked(numKeys)
	packed = append(packed, common.Hash{1}) // Invalidate the packing

	test := precompiletest.MessageTest{
		VerifyContext: &warpmsg.VerifyContext{
			SnowCtx: snowCtx,
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		Packed: packed,
		Rules:  graniteRules,
		Gas:    GraniteGasConfig.MessageGasCost(len(packed), numKeys),
		GasErr: ErrInvalidPacked,
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
	packed := warpmsg.Pack(warpMsgBytes)

	test := precompiletest.MessageTest{
		VerifyContext: &warpmsg.VerifyContext{
			SnowCtx: snowCtx,
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		Packed: packed,
		Rules:  graniteRules,
		Gas:    GraniteGasConfig.MessageGasCost(len(packed), numKeys),
		GasErr: ErrInvalidWarpMsg,
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
	packed := warpmsg.Pack(warpMsgBytes)

	test := precompiletest.MessageTest{
		VerifyContext: &warpmsg.VerifyContext{
			SnowCtx: snowCtx,
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		Packed: packed,
		Rules:  graniteRules,
		Gas:    GraniteGasConfig.MessageGasCost(len(packed), numKeys),
		GasErr: ErrInvalidWarpMsgPayload,
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
	packed := warpmsg.Pack(msg.Bytes())
	test := precompiletest.MessageTest{
		VerifyContext: &warpmsg.VerifyContext{
			SnowCtx: snowCtx,
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		Packed: packed,
		Rules:  graniteRules,
		Gas:    GraniteGasConfig.MessageGasCost(len(packed), numKeys),
		GasErr: ErrCannotGetNumSigners,
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

	var tests []precompiletest.MessageTest
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
		packed := createPacked(tt.numSigners)
		tests = append(tests, precompiletest.MessageTest{
			Name: fmt.Sprintf("%d_signers", tt.numSigners),
			VerifyContext: &warpmsg.VerifyContext{
				SnowCtx: snowCtx,
				ProposerVMBlockCtx: &block.Context{
					PChainHeight: 1,
				},
			},
			Packed:      packed,
			Rules:       graniteRules,
			Gas:         GraniteGasConfig.MessageGasCost(len(packed), tt.numSigners),
			ExpectedErr: tt.wantErr,
		})
	}
	precompiletest.RunMessageTests(t, tests)
}

// multiple messages all correct, multiple messages all incorrect, mixed bag
func TestWarpMultipleMessages(t *testing.T) {
	snowCtx := createSnowCtx(t, []validatorRange{
		{
			start:     0,
			end:       100,
			weight:    20,
			publicKey: true,
		},
	})

	var tests []precompiletest.MessageTest
	for _, validMessageIndices := range [][]bool{
		{},
		{true, false},
		{false, true},
		{false, false},
		{true, true},
	} {
		var (
			numSigners    = int(WarpQuorumDenominator)
			invalidPacked = createPacked(1)
			validPacked   = createPacked(numSigners)
		)

		for _, valid := range validMessageIndices {
			var (
				packed      warpmsg.Packed
				expectedGas uint64
				expectedErr error
			)
			if valid {
				packed = validPacked
				expectedGas = GraniteGasConfig.MessageGasCost(len(packed), numSigners)
				expectedErr = nil
			} else {
				packed = invalidPacked
				expectedGas = GraniteGasConfig.MessageGasCost(len(invalidPacked), 1)
				expectedErr = ErrFailedVerification
			}

			tests = append(tests, precompiletest.MessageTest{
				Name: fmt.Sprintf("multiple messages %v", validMessageIndices),
				VerifyContext: &warpmsg.VerifyContext{
					SnowCtx: snowCtx,
					ProposerVMBlockCtx: &block.Context{
						PChainHeight: 1,
					},
				},
				Packed:      packed,
				Rules:       graniteRules,
				Gas:         expectedGas,
				GasErr:      nil,
				ExpectedErr: expectedErr,
			})
		}
	}
	precompiletest.RunMessageTests(t, tests)
}

func TestWarpNoValidatorsAndOverflowUseSameGas(t *testing.T) {
	var (
		proposervmContext = &block.Context{
			PChainHeight: 1,
		}
		packed      = createPacked(0)
		expectedGas = GraniteGasConfig.MessageGasCost(len(packed), 0)
	)
	noValidators := precompiletest.MessageTest{
		Name: "no_validators",
		VerifyContext: &warpmsg.VerifyContext{
			SnowCtx:            createSnowCtx(t, nil /*=validators*/), // No validators in state
			ProposerVMBlockCtx: proposervmContext,
		},
		Rules:       graniteRules,
		Packed:      packed,
		Gas:         expectedGas,
		GasErr:      nil,
		ExpectedErr: bls.ErrNoPublicKeys,
	}
	weightOverflow := precompiletest.MessageTest{
		Name: "weight_overflow",
		VerifyContext: &warpmsg.VerifyContext{
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
		Packed:      packed,
		Rules:       graniteRules,
		Gas:         expectedGas,
		GasErr:      nil,
		ExpectedErr: safemath.ErrOverflow,
	}
	precompiletest.RunMessageTests(t, []precompiletest.MessageTest{
		noValidators,
		weightOverflow,
	})
}

func makeWarpMessageTests(tb testing.TB, rules cparams.Rules) []precompiletest.MessageTest {
	allTotalNodes := []int{10, 100, 1_000, 10_000}
	tests := make([]precompiletest.MessageTest, len(allTotalNodes))
	for i, totalNodes := range allTotalNodes {
		packed := createPacked(totalNodes)
		snowCtx := createSnowCtx(tb, []validatorRange{
			{
				start:     0,
				end:       totalNodes,
				weight:    20,
				publicKey: true,
			},
		})
		test := createValidMessageTest(snowCtx, totalNodes, packed, rules)
		test.Name = fmt.Sprintf("%d signers/%d validators", totalNodes, totalNodes)
		tests[i] = test
	}

	numSigners := 10
	for _, totalNodes := range []int{100, 1_000, 10_000} {
		packed := createPacked(numSigners)
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
		test := createValidMessageTest(snowCtx, numSigners, packed, rules)
		test.Name = fmt.Sprintf("%d signers (heavily weighted)/%d validators", numSigners, totalNodes)
		tests = append(tests, test)
	}

	for _, totalNodes := range []int{100, 1_000, 10_000} {
		packed := createPacked(numSigners)
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
		test := createValidMessageTest(snowCtx, numSigners, packed, rules)
		test.Name = fmt.Sprintf("%d signers (heavily weighted)/%d validators (non-signers without registered PublicKey)", numSigners, totalNodes)
		tests = append(tests, test)
	}

	for _, totalNodes := range []int{100, 1_000, 10_000} {
		packed := createPacked(numSigners)
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

		test := createValidMessageTest(snowCtx, numSigners, packed, rules)
		test.Name = fmt.Sprintf("%d validators w/ %d signers/repeated PublicKeys", totalNodes, numSigners)
		tests = append(tests, test)
	}
	return tests
}

func TestWarpMessage(t *testing.T) {
	for _, fork := range forks {
		t.Run(fork.String(), func(t *testing.T) {
			rules := forkToAvalancheRules(fork)
			tests := makeWarpMessageTests(t, rules)
			precompiletest.RunMessageTests(t, tests)
		})
	}
}

func BenchmarkWarpMessage(b *testing.B) {
	for _, fork := range forks {
		b.Run(fork.String(), func(b *testing.B) {
			rules := forkToAvalancheRules(fork)
			tests := makeWarpMessageTests(b, rules)
			precompiletest.RunMessageBenchmarks(b, tests)
		})
	}
}
