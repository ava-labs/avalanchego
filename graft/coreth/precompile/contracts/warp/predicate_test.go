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
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/precompile/precompileconfig"
	"github.com/ava-labs/coreth/precompile/testutils"
	"github.com/ava-labs/coreth/predicate"
	corethUtils "github.com/ava-labs/coreth/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

const pChainHeight uint64 = 1337

var (
	_ utils.Sortable[*testValidator] = (*testValidator)(nil)

	errTest        = errors.New("non-nil error")
	networkID      = uint32(54321)
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
	tests       []signatureTest

	predicateTests = make(map[string]testutils.PredicateTest)
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
	unsignedMsg, err = avalancheWarp.NewUnsignedMessage(networkID, sourceChainID, addressedPayload.Bytes())
	if err != nil {
		panic(err)
	}

	for _, testVdr := range testVdrs {
		blsSignature := bls.Sign(testVdr.sk, unsignedMsg.Bytes())
		blsSignatures = append(blsSignatures, blsSignature)
	}

	initWarpPredicateTests()
}

type testValidator struct {
	nodeID ids.NodeID
	sk     *bls.SecretKey
	vdr    *avalancheWarp.Validator
}

func (v *testValidator) Less(o *testValidator) bool {
	return v.vdr.Less(o.vdr)
}

func newTestValidator() *testValidator {
	sk, err := bls.NewSecretKey()
	if err != nil {
		panic(err)
	}

	nodeID := ids.GenerateTestNodeID()
	pk := bls.PublicFromSecretKey(sk)
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

type signatureTest struct {
	name      string
	stateF    func(*gomock.Controller) validators.State
	quorumNum uint64
	quorumDen uint64
	msgF      func(*require.Assertions) *avalancheWarp.Message
	err       error
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
func createSnowCtx(validatorRanges []validatorRange) *snow.Context {
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

	snowCtx := snow.DefaultContextTest()
	state := &validators.TestState{
		GetSubnetIDF: func(ctx context.Context, chainID ids.ID) (ids.ID, error) {
			return sourceSubnetID, nil
		},
		GetValidatorSetF: func(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			return getValidatorsOutput, nil
		},
	}
	snowCtx.ValidatorState = state
	snowCtx.NetworkID = networkID
	return snowCtx
}

func createValidPredicateTest(snowCtx *snow.Context, numKeys uint64, predicateBytes []byte) testutils.PredicateTest {
	return testutils.PredicateTest{
		Config: NewDefaultConfig(corethUtils.NewUint64(0)),
		PredicateContext: &precompileconfig.PredicateContext{
			SnowCtx: snowCtx,
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		StorageSlots: [][]byte{predicateBytes},
		Gas:          GasCostPerSignatureVerification + uint64(len(predicateBytes))*GasCostPerWarpMessageBytes + numKeys*GasCostPerWarpSigner,
		GasErr:       nil,
		PredicateRes: set.NewBits().Bytes(),
	}
}

func TestWarpMessageFromPrimaryNetwork(t *testing.T) {
	require := require.New(t)
	numKeys := 10
	cChainID := ids.GenerateTestID()
	addressedCall, err := payload.NewAddressedCall(utils.RandomBytes(20), utils.RandomBytes(100))
	require.NoError(err)
	unsignedMsg, err := avalancheWarp.NewUnsignedMessage(networkID, cChainID, addressedCall.Bytes())
	require.NoError(err)

	getValidatorsOutput := make(map[ids.NodeID]*validators.GetValidatorOutput)
	blsSignatures := make([]*bls.Signature, 0, numKeys)
	for i := 0; i < numKeys; i++ {
		validatorOutput := &validators.GetValidatorOutput{
			NodeID:    testVdrs[i].nodeID,
			Weight:    20,
			PublicKey: testVdrs[i].vdr.PublicKey,
		}
		getValidatorsOutput[testVdrs[i].nodeID] = validatorOutput
		blsSignatures = append(blsSignatures, bls.Sign(testVdrs[i].sk, unsignedMsg.Bytes()))
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

	snowCtx := snow.DefaultContextTest()
	snowCtx.SubnetID = ids.GenerateTestID()
	snowCtx.ChainID = ids.GenerateTestID()
	snowCtx.CChainID = cChainID
	snowCtx.NetworkID = networkID
	snowCtx.ValidatorState = &validators.TestState{
		GetSubnetIDF: func(ctx context.Context, chainID ids.ID) (ids.ID, error) {
			require.Equal(chainID, cChainID)
			return constants.PrimaryNetworkID, nil // Return Primary Network SubnetID
		},
		GetValidatorSetF: func(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			require.Equal(snowCtx.SubnetID, subnetID)
			return getValidatorsOutput, nil
		},
	}

	test := testutils.PredicateTest{
		Config: NewDefaultConfig(corethUtils.NewUint64(0)),
		PredicateContext: &precompileconfig.PredicateContext{
			SnowCtx: snowCtx,
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		StorageSlots: [][]byte{predicateBytes},
		Gas:          GasCostPerSignatureVerification + uint64(len(predicateBytes))*GasCostPerWarpMessageBytes + uint64(numKeys)*GasCostPerWarpSigner,
		GasErr:       nil,
		PredicateRes: set.NewBits().Bytes(),
	}

	test.Run(t)
}

func TestInvalidPredicatePacking(t *testing.T) {
	numKeys := 1
	snowCtx := createSnowCtx([]validatorRange{
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
		Config: NewDefaultConfig(corethUtils.NewUint64(0)),
		PredicateContext: &precompileconfig.PredicateContext{
			SnowCtx: snowCtx,
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		StorageSlots: [][]byte{predicateBytes},
		Gas:          GasCostPerSignatureVerification + uint64(len(predicateBytes))*GasCostPerWarpMessageBytes + uint64(numKeys)*GasCostPerWarpSigner,
		GasErr:       errInvalidPredicateBytes,
	}

	test.Run(t)
}

func TestInvalidWarpMessage(t *testing.T) {
	numKeys := 1
	snowCtx := createSnowCtx([]validatorRange{
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
		Config: NewDefaultConfig(corethUtils.NewUint64(0)),
		PredicateContext: &precompileconfig.PredicateContext{
			SnowCtx: snowCtx,
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		StorageSlots: [][]byte{predicateBytes},
		Gas:          GasCostPerSignatureVerification + uint64(len(predicateBytes))*GasCostPerWarpMessageBytes + uint64(numKeys)*GasCostPerWarpSigner,
		GasErr:       errInvalidWarpMsg,
		PredicateRes: set.NewBits(0).Bytes(), // Won't be reached
	}

	test.Run(t)
}

func TestInvalidAddressedPayload(t *testing.T) {
	numKeys := 1
	snowCtx := createSnowCtx([]validatorRange{
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
	unsignedMsg, err := avalancheWarp.NewUnsignedMessage(networkID, sourceChainID, []byte{1, 2, 3})
	require.NoError(t, err)
	warpMsg, err := avalancheWarp.NewMessage(unsignedMsg, warpSignature)
	require.NoError(t, err)
	warpMsgBytes := warpMsg.Bytes()
	predicateBytes := predicate.PackPredicate(warpMsgBytes)

	test := testutils.PredicateTest{
		Config: NewDefaultConfig(corethUtils.NewUint64(0)),
		PredicateContext: &precompileconfig.PredicateContext{
			SnowCtx: snowCtx,
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		StorageSlots: [][]byte{predicateBytes},
		Gas:          GasCostPerSignatureVerification + uint64(len(predicateBytes))*GasCostPerWarpMessageBytes + uint64(numKeys)*GasCostPerWarpSigner,
		GasErr:       errInvalidWarpMsgPayload,
	}

	test.Run(t)
}

func TestInvalidBitSet(t *testing.T) {
	addressedCall, err := payload.NewAddressedCall(utils.RandomBytes(20), utils.RandomBytes(100))
	require.NoError(t, err)
	unsignedMsg, err := avalancheWarp.NewUnsignedMessage(
		networkID,
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
	snowCtx := createSnowCtx([]validatorRange{
		{
			start:     0,
			end:       numKeys,
			weight:    20,
			publicKey: true,
		},
	})
	predicateBytes := predicate.PackPredicate(msg.Bytes())
	test := testutils.PredicateTest{
		Config: NewDefaultConfig(corethUtils.NewUint64(0)),
		PredicateContext: &precompileconfig.PredicateContext{
			SnowCtx: snowCtx,
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		StorageSlots: [][]byte{predicateBytes},
		Gas:          GasCostPerSignatureVerification + uint64(len(predicateBytes))*GasCostPerWarpMessageBytes + uint64(numKeys)*GasCostPerWarpSigner,
		GasErr:       errCannotGetNumSigners,
		PredicateRes: set.NewBits(0).Bytes(), // Won't be reached
	}

	test.Run(t)
}

func TestWarpSignatureWeightsDefaultQuorumNumerator(t *testing.T) {
	snowCtx := createSnowCtx([]validatorRange{
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
		int(params.WarpDefaultQuorumNumerator) - 1,
		int(params.WarpDefaultQuorumNumerator),
		int(params.WarpDefaultQuorumNumerator) + 1,
		int(params.WarpQuorumDenominator) - 1,
		int(params.WarpQuorumDenominator),
		int(params.WarpQuorumDenominator) + 1,
	} {
		var (
			predicateBytes           = createPredicate(numSigners)
			expectedPredicateResults = set.NewBits()
		)
		// If the number of signers is less than the required numerator or exceeds the denominator, then
		// mark the expected result as invalid.
		if numSigners < int(params.WarpDefaultQuorumNumerator) || numSigners > int(params.WarpQuorumDenominator) {
			expectedPredicateResults.Add(0)
		}
		tests[fmt.Sprintf("default quorum %d signature(s)", numSigners)] = testutils.PredicateTest{
			Config: NewDefaultConfig(corethUtils.NewUint64(0)),
			PredicateContext: &precompileconfig.PredicateContext{
				SnowCtx: snowCtx,
				ProposerVMBlockCtx: &block.Context{
					PChainHeight: 1,
				},
			},
			StorageSlots: [][]byte{predicateBytes},
			Gas:          GasCostPerSignatureVerification + uint64(len(predicateBytes))*GasCostPerWarpMessageBytes + uint64(numSigners)*GasCostPerWarpSigner,
			GasErr:       nil,
			PredicateRes: expectedPredicateResults.Bytes(),
		}
	}
	testutils.RunPredicateTests(t, tests)
}

// multiple messages all correct, multiple messages all incorrect, mixed bag
func TestWarpMultiplePredicates(t *testing.T) {
	snowCtx := createSnowCtx([]validatorRange{
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
			numSigners               = int(params.WarpQuorumDenominator)
			invalidPredicateBytes    = createPredicate(1)
			validPredicateBytes      = createPredicate(numSigners)
			expectedPredicateResults = set.NewBits()
		)
		predicates := make([][]byte, len(validMessageIndices))
		expectedGas := uint64(0)
		for index, valid := range validMessageIndices {
			if valid {
				predicates[index] = common.CopyBytes(validPredicateBytes)
				expectedGas += GasCostPerSignatureVerification + uint64(len(validPredicateBytes))*GasCostPerWarpMessageBytes + uint64(numSigners)*GasCostPerWarpSigner
			} else {
				expectedPredicateResults.Add(index)
				expectedGas += GasCostPerSignatureVerification + uint64(len(invalidPredicateBytes))*GasCostPerWarpMessageBytes + uint64(1)*GasCostPerWarpSigner
				predicates[index] = invalidPredicateBytes
			}
		}

		tests[fmt.Sprintf("multiple predicates %v", validMessageIndices)] = testutils.PredicateTest{
			Config: NewDefaultConfig(corethUtils.NewUint64(0)),
			PredicateContext: &precompileconfig.PredicateContext{
				SnowCtx: snowCtx,
				ProposerVMBlockCtx: &block.Context{
					PChainHeight: 1,
				},
			},
			StorageSlots: predicates,
			Gas:          expectedGas,
			GasErr:       nil,
			PredicateRes: expectedPredicateResults.Bytes(),
		}
	}
	testutils.RunPredicateTests(t, tests)
}

func TestWarpSignatureWeightsNonDefaultQuorumNumerator(t *testing.T) {
	snowCtx := createSnowCtx([]validatorRange{
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
	require.NotEqual(t, nonDefaultQuorumNumerator, int(params.WarpDefaultQuorumNumerator))
	// Add cases with default quorum
	for _, numSigners := range []int{nonDefaultQuorumNumerator, nonDefaultQuorumNumerator + 1, 99, 100, 101} {
		var (
			predicateBytes           = createPredicate(numSigners)
			expectedPredicateResults = set.NewBits()
		)
		// If the number of signers is less than the required numerator or exceeds the denominator, then
		// mark the expected result as invalid.
		if numSigners < nonDefaultQuorumNumerator || numSigners > int(params.WarpQuorumDenominator) {
			expectedPredicateResults.Add(0)
		}
		name := fmt.Sprintf("non-default quorum %d signature(s)", numSigners)
		tests[name] = testutils.PredicateTest{
			Config: NewConfig(corethUtils.NewUint64(0), uint64(nonDefaultQuorumNumerator)),
			PredicateContext: &precompileconfig.PredicateContext{
				SnowCtx: snowCtx,
				ProposerVMBlockCtx: &block.Context{
					PChainHeight: 1,
				},
			},
			StorageSlots: [][]byte{predicateBytes},
			Gas:          GasCostPerSignatureVerification + uint64(len(predicateBytes))*GasCostPerWarpMessageBytes + uint64(numSigners)*GasCostPerWarpSigner,
			GasErr:       nil,
			PredicateRes: expectedPredicateResults.Bytes(),
		}
	}

	testutils.RunPredicateTests(t, tests)
}

func initWarpPredicateTests() {
	for _, totalNodes := range []int{10, 100, 1_000, 10_000} {
		testName := fmt.Sprintf("%d signers/%d validators", totalNodes, totalNodes)

		predicateBytes := createPredicate(totalNodes)
		snowCtx := createSnowCtx([]validatorRange{
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
		snowCtx := createSnowCtx([]validatorRange{
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
		snowCtx := createSnowCtx([]validatorRange{
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

		snowCtx := snow.DefaultContextTest()
		snowCtx.NetworkID = networkID
		state := &validators.TestState{
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
}

func TestWarpPredicate(t *testing.T) {
	testutils.RunPredicateTests(t, predicateTests)
}

func BenchmarkWarpPredicate(b *testing.B) {
	testutils.RunPredicateBenchmarks(b, predicateTests)
}
