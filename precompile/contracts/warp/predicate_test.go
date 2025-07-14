// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	agoUtils "github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/set"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/subnet-evm/precompile/testutils"
	"github.com/ava-labs/subnet-evm/predicate"
	"github.com/ava-labs/subnet-evm/utils"
	"github.com/ava-labs/subnet-evm/utils/utilstest"
	"github.com/stretchr/testify/require"
)

const pChainHeight uint64 = 1337

var (
	_ agoUtils.Sortable[*testValidator] = (*testValidator)(nil)

	errTest        = errors.New("non-nil error")
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

// createWarpMessage constructs a signed warp message using the global variable [unsignedMsg]
// and the first [numKeys] signatures from [blsSignatures]
func createWarpMessage(numKeys int) *avalancheWarp.Message {
	aggregateSignature, err := bls.AggregateSignatures(blsSignatures[0:numKeys])
	if err != nil {
		panic(err)
	}
	bitSet := set.NewBits()
	for i := 0; i < numKeys; i++ {
		bitSet.Add(i)
	}
	warpSignature := &avalancheWarp.BitSetSignature{
		Signers: bitSet.Bytes(),
	}
	copy(warpSignature.Signature[:], bls.SignatureToBytes(aggregateSignature))
	warpMsg, err := avalancheWarp.NewMessage(unsignedMsg, warpSignature)
	if err != nil {
		panic(err)
	}
	return warpMsg
}

// createPredicate constructs a warp message using createWarpMessage with numKeys signers
// and packs it into predicate encoding.
func createPredicate(numKeys int) []byte {
	warpMsg := createWarpMessage(numKeys)
	predicateBytes := predicate.PackPredicate(warpMsg.Bytes())
	return predicateBytes
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
	getValidatorsOutput := make(map[ids.NodeID]*validators.GetValidatorOutput)

	for _, validatorRange := range validatorRanges {
		for i := validatorRange.start; i < validatorRange.end; i++ {
			validatorOutput := &validators.GetValidatorOutput{
				NodeID: testVdrs[i].nodeID,
				Weight: validatorRange.weight,
			}
			if validatorRange.publicKey {
				validatorOutput.PublicKey = testVdrs[i].vdr.PublicKey
			}
			getValidatorsOutput[testVdrs[i].nodeID] = validatorOutput
		}
	}

	snowCtx := utilstest.NewTestSnowContext(tb)
	state := &validatorstest.State{
		GetSubnetIDF: func(ctx context.Context, chainID ids.ID) (ids.ID, error) {
			return sourceSubnetID, nil
		},
		GetValidatorSetF: func(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			return getValidatorsOutput, nil
		},
	}
	snowCtx.ValidatorState = state
	return snowCtx
}

func createValidPredicateTest(snowCtx *snow.Context, numKeys uint64, predicateBytes []byte) testutils.PredicateTest {
	return testutils.PredicateTest{
		Config: NewDefaultConfig(utils.NewUint64(0)),
		PredicateContext: &precompileconfig.PredicateContext{
			SnowCtx: snowCtx,
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		PredicateBytes: predicateBytes,
		Gas:            GasCostPerSignatureVerification + uint64(len(predicateBytes))*GasCostPerWarpMessageBytes + numKeys*GasCostPerWarpSigner,
		GasErr:         nil,
		ExpectedErr:    nil,
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

	getValidatorsOutput := make(map[ids.NodeID]*validators.GetValidatorOutput)
	blsSignatures := make([]*bls.Signature, 0, numKeys)
	for i := 0; i < numKeys; i++ {
		sig, err := testVdrs[i].sk.Sign(unsignedMsg.Bytes())
		require.NoError(err)

		validatorOutput := &validators.GetValidatorOutput{
			NodeID:    testVdrs[i].nodeID,
			Weight:    20,
			PublicKey: testVdrs[i].vdr.PublicKey,
		}
		getValidatorsOutput[testVdrs[i].nodeID] = validatorOutput
		blsSignatures = append(blsSignatures, sig)
	}
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

	predicateBytes := predicate.PackPredicate(warpMsg.Bytes())

	snowCtx := utilstest.NewTestSnowContext(t)
	snowCtx.SubnetID = ids.GenerateTestID()
	snowCtx.ChainID = ids.GenerateTestID()
	snowCtx.CChainID = cChainID
	snowCtx.ValidatorState = &validatorstest.State{
		GetSubnetIDF: func(ctx context.Context, chainID ids.ID) (ids.ID, error) {
			require.Equal(chainID, cChainID)
			return constants.PrimaryNetworkID, nil // Return Primary Network SubnetID
		},
		GetValidatorSetF: func(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			expectedSubnetID := snowCtx.SubnetID
			if requirePrimaryNetworkSigners {
				expectedSubnetID = constants.PrimaryNetworkID
			}
			require.Equal(expectedSubnetID, subnetID)
			return getValidatorsOutput, nil
		},
	}

	test := testutils.PredicateTest{
		Config: NewConfig(utils.NewUint64(0), 0, requirePrimaryNetworkSigners),
		PredicateContext: &precompileconfig.PredicateContext{
			SnowCtx: snowCtx,
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		PredicateBytes: predicateBytes,
		Gas:            GasCostPerSignatureVerification + uint64(len(predicateBytes))*GasCostPerWarpMessageBytes + uint64(numKeys)*GasCostPerWarpSigner,
		GasErr:         nil,
		ExpectedErr:    nil,
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
	predicateBytes := createPredicate(numKeys)
	predicateBytes = append(predicateBytes, byte(0x01)) // Invalidate the predicate byte packing

	test := testutils.PredicateTest{
		Config: NewDefaultConfig(utils.NewUint64(0)),
		PredicateContext: &precompileconfig.PredicateContext{
			SnowCtx: snowCtx,
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		PredicateBytes: predicateBytes,
		Gas:            GasCostPerSignatureVerification + uint64(len(predicateBytes))*GasCostPerWarpMessageBytes + uint64(numKeys)*GasCostPerWarpSigner,
		GasErr:         errInvalidPredicateBytes,
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
	predicateBytes := predicate.PackPredicate(warpMsgBytes)

	test := testutils.PredicateTest{
		Config: NewDefaultConfig(utils.NewUint64(0)),
		PredicateContext: &precompileconfig.PredicateContext{
			SnowCtx: snowCtx,
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		PredicateBytes: predicateBytes,
		Gas:            GasCostPerSignatureVerification + uint64(len(predicateBytes))*GasCostPerWarpMessageBytes + uint64(numKeys)*GasCostPerWarpSigner,
		GasErr:         errInvalidWarpMsg,
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
	predicateBytes := predicate.PackPredicate(warpMsgBytes)

	test := testutils.PredicateTest{
		Config: NewDefaultConfig(utils.NewUint64(0)),
		PredicateContext: &precompileconfig.PredicateContext{
			SnowCtx: snowCtx,
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		PredicateBytes: predicateBytes,
		Gas:            GasCostPerSignatureVerification + uint64(len(predicateBytes))*GasCostPerWarpMessageBytes + uint64(numKeys)*GasCostPerWarpSigner,
		GasErr:         errInvalidWarpMsgPayload,
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
	predicateBytes := predicate.PackPredicate(msg.Bytes())
	test := testutils.PredicateTest{
		Config: NewDefaultConfig(utils.NewUint64(0)),
		PredicateContext: &precompileconfig.PredicateContext{
			SnowCtx: snowCtx,
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		PredicateBytes: predicateBytes,
		Gas:            GasCostPerSignatureVerification + uint64(len(predicateBytes))*GasCostPerWarpMessageBytes + uint64(numKeys)*GasCostPerWarpSigner,
		GasErr:         errCannotGetNumSigners,
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

	tests := make(map[string]testutils.PredicateTest)
	for _, numSigners := range []int{
		1,
		int(WarpDefaultQuorumNumerator) - 1,
		int(WarpDefaultQuorumNumerator),
		int(WarpDefaultQuorumNumerator) + 1,
		int(WarpQuorumDenominator) - 1,
		int(WarpQuorumDenominator),
		int(WarpQuorumDenominator) + 1,
	} {
		predicateBytes := createPredicate(numSigners)
		// The predicate is valid iff the number of signers is >= the required numerator and does not exceed the denominator.
		var expectedErr error
		if numSigners >= int(WarpDefaultQuorumNumerator) && numSigners <= int(WarpQuorumDenominator) {
			expectedErr = nil
		} else {
			expectedErr = errFailedVerification
		}

		tests[fmt.Sprintf("default quorum %d signature(s)", numSigners)] = testutils.PredicateTest{
			Config: NewDefaultConfig(utils.NewUint64(0)),
			PredicateContext: &precompileconfig.PredicateContext{
				SnowCtx: snowCtx,
				ProposerVMBlockCtx: &block.Context{
					PChainHeight: 1,
				},
			},
			PredicateBytes: predicateBytes,
			Gas:            GasCostPerSignatureVerification + uint64(len(predicateBytes))*GasCostPerWarpMessageBytes + uint64(numSigners)*GasCostPerWarpSigner,
			GasErr:         nil,
			ExpectedErr:    expectedErr,
		}
	}
	testutils.RunPredicateTests(t, tests)
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

	tests := make(map[string]testutils.PredicateTest)
	for _, validMessageIndices := range [][]bool{
		{},
		{true, false},
		{false, true},
		{false, false},
		{true, true},
	} {
		var (
			numSigners            = int(WarpQuorumDenominator)
			invalidPredicateBytes = createPredicate(1)
			validPredicateBytes   = createPredicate(numSigners)
		)

		for _, valid := range validMessageIndices {
			var (
				predicate   []byte
				expectedGas uint64
				expectedErr error
			)
			if valid {
				predicate = validPredicateBytes
				expectedGas = GasCostPerSignatureVerification + uint64(len(validPredicateBytes))*GasCostPerWarpMessageBytes + uint64(numSigners)*GasCostPerWarpSigner
				expectedErr = nil
			} else {
				expectedGas = GasCostPerSignatureVerification + uint64(len(invalidPredicateBytes))*GasCostPerWarpMessageBytes + uint64(1)*GasCostPerWarpSigner
				predicate = invalidPredicateBytes
				expectedErr = errFailedVerification
			}

			tests[fmt.Sprintf("multiple predicates %v", validMessageIndices)] = testutils.PredicateTest{
				Config: NewDefaultConfig(utils.NewUint64(0)),
				PredicateContext: &precompileconfig.PredicateContext{
					SnowCtx: snowCtx,
					ProposerVMBlockCtx: &block.Context{
						PChainHeight: 1,
					},
				},
				PredicateBytes: predicate,
				Gas:            expectedGas,
				GasErr:         nil,
				ExpectedErr:    expectedErr,
			}
		}
	}
	testutils.RunPredicateTests(t, tests)
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

	tests := make(map[string]testutils.PredicateTest)
	nonDefaultQuorumNumerator := 50
	// Ensure this test fails if the DefaultQuroumNumerator is changed to an unexpected value during development
	require.NotEqual(t, nonDefaultQuorumNumerator, int(WarpDefaultQuorumNumerator))
	// Add cases with default quorum
	for _, numSigners := range []int{nonDefaultQuorumNumerator, nonDefaultQuorumNumerator + 1, 99, 100, 101} {
		predicateBytes := createPredicate(numSigners)
		// The predicate is valid iff the number of signers is >= the required numerator and does not exceed the denominator.
		var expectedErr error
		if numSigners >= nonDefaultQuorumNumerator && numSigners <= int(WarpQuorumDenominator) {
			expectedErr = nil
		} else {
			expectedErr = errFailedVerification
		}

		name := fmt.Sprintf("non-default quorum %d signature(s)", numSigners)
		tests[name] = testutils.PredicateTest{
			Config: NewConfig(utils.NewUint64(0), uint64(nonDefaultQuorumNumerator), false),
			PredicateContext: &precompileconfig.PredicateContext{
				SnowCtx: snowCtx,
				ProposerVMBlockCtx: &block.Context{
					PChainHeight: 1,
				},
			},
			PredicateBytes: predicateBytes,
			Gas:            GasCostPerSignatureVerification + uint64(len(predicateBytes))*GasCostPerWarpMessageBytes + uint64(numSigners)*GasCostPerWarpSigner,
			GasErr:         nil,
			ExpectedErr:    expectedErr,
		}
	}

	testutils.RunPredicateTests(t, tests)
}

func makeWarpPredicateTests(tb testing.TB) map[string]testutils.PredicateTest {
	predicateTests := make(map[string]testutils.PredicateTest)
	for _, totalNodes := range []int{10, 100, 1_000, 10_000} {
		testName := fmt.Sprintf("%d signers/%d validators", totalNodes, totalNodes)

		predicateBytes := createPredicate(totalNodes)
		snowCtx := createSnowCtx(tb, []validatorRange{
			{
				start:     0,
				end:       totalNodes,
				weight:    20,
				publicKey: true,
			},
		})
		predicateTests[testName] = createValidPredicateTest(snowCtx, uint64(totalNodes), predicateBytes)
	}

	numSigners := 10
	for _, totalNodes := range []int{100, 1_000, 10_000} {
		testName := fmt.Sprintf("%d signers (heavily weighted)/%d validators", numSigners, totalNodes)

		predicateBytes := createPredicate(numSigners)
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
		predicateTests[testName] = createValidPredicateTest(snowCtx, uint64(numSigners), predicateBytes)
	}

	for _, totalNodes := range []int{100, 1_000, 10_000} {
		testName := fmt.Sprintf("%d signers (heavily weighted)/%d validators (non-signers without registered PublicKey)", numSigners, totalNodes)

		predicateBytes := createPredicate(numSigners)
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
		predicateTests[testName] = createValidPredicateTest(snowCtx, uint64(numSigners), predicateBytes)
	}

	for _, totalNodes := range []int{100, 1_000, 10_000} {
		testName := fmt.Sprintf("%d validators w/ %d signers/repeated PublicKeys", totalNodes, numSigners)

		predicateBytes := createPredicate(numSigners)
		getValidatorsOutput := make(map[ids.NodeID]*validators.GetValidatorOutput, totalNodes)
		for i := 0; i < totalNodes; i++ {
			getValidatorsOutput[testVdrs[i].nodeID] = &validators.GetValidatorOutput{
				NodeID:    testVdrs[i].nodeID,
				Weight:    20,
				PublicKey: testVdrs[i%numSigners].vdr.PublicKey,
			}
		}

		snowCtx := utilstest.NewTestSnowContext(tb)

		state := &validatorstest.State{
			GetSubnetIDF: func(ctx context.Context, chainID ids.ID) (ids.ID, error) {
				return sourceSubnetID, nil
			},
			GetValidatorSetF: func(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
				return getValidatorsOutput, nil
			},
		}
		snowCtx.ValidatorState = state

		predicateTests[testName] = createValidPredicateTest(snowCtx, uint64(numSigners), predicateBytes)
	}
	return predicateTests
}

func TestWarpPredicate(t *testing.T) {
	predicateTests := makeWarpPredicateTests(t)
	testutils.RunPredicateTests(t, predicateTests)
}

func BenchmarkWarpPredicate(b *testing.B) {
	predicateTests := makeWarpPredicateTests(b)
	testutils.RunPredicateBenchmarks(b, predicateTests)
}
