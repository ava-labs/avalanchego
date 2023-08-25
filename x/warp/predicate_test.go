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
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/subnet-evm/precompile/testutils"
	subnetEVMUtils "github.com/ava-labs/subnet-evm/utils"
	predicateutils "github.com/ava-labs/subnet-evm/utils/predicate"
	warpPayload "github.com/ava-labs/subnet-evm/warp/payload"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

const pChainHeight uint64 = 1337

var (
	_ utils.Sortable[*testValidator] = (*testValidator)(nil)

	errTest            = errors.New("non-nil error")
	networkID          = uint32(54321)
	sourceChainID      = ids.GenerateTestID()
	sourceSubnetID     = ids.GenerateTestID()
	destinationChainID = ids.GenerateTestID()

	// valid unsigned warp message used throughout testing
	unsignedMsg *avalancheWarp.UnsignedMessage
	// valid addressed payload
	addressedPayload      *warpPayload.AddressedPayload
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
	addressedPayload, err = warpPayload.NewAddressedPayload(
		common.Address(ids.GenerateTestShortID()),
		common.Hash(destinationChainID),
		common.Address(ids.GenerateTestShortID()),
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
	predicateBytes := predicateutils.PackPredicate(warpMsg.Bytes())
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
		Config: NewDefaultConfig(subnetEVMUtils.NewUint64(0)),
		ProposerPredicateContext: &precompileconfig.ProposerPredicateContext{
			PrecompilePredicateContext: precompileconfig.PrecompilePredicateContext{
				SnowCtx: snowCtx,
			},
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		StorageSlots: predicateBytes,
		Gas:          GasCostPerSignatureVerification + uint64(len(predicateBytes))*GasCostPerWarpMessageBytes + numKeys*GasCostPerWarpSigner,
		GasErr:       nil,
		PredicateErr: nil,
	}
}

func TestWarpNilProposerCtx(t *testing.T) {
	numKeys := 1
	snowCtx := createSnowCtx([]validatorRange{
		{
			start:  0,
			end:    numKeys,
			weight: 20,
		},
	})
	predicateBytes := createPredicate(numKeys)
	test := testutils.PredicateTest{
		Config: NewDefaultConfig(subnetEVMUtils.NewUint64(0)),
		ProposerPredicateContext: &precompileconfig.ProposerPredicateContext{
			PrecompilePredicateContext: precompileconfig.PrecompilePredicateContext{
				SnowCtx: snowCtx,
			},
			ProposerVMBlockCtx: nil,
		},
		StorageSlots: predicateBytes,
		Gas:          GasCostPerSignatureVerification + uint64(len(predicateBytes))*GasCostPerWarpMessageBytes + uint64(numKeys)*GasCostPerWarpSigner,
		GasErr:       nil,
		PredicateErr: errNoProposerCtxPredicate,
	}

	test.Run(t)
}

func TestInvalidPredicatePacking(t *testing.T) {
	numKeys := 1
	snowCtx := createSnowCtx([]validatorRange{
		{
			start:  0,
			end:    numKeys,
			weight: 20,
		},
	})
	predicateBytes := createPredicate(numKeys)
	predicateBytes = append(predicateBytes, byte(0x01)) // Invalidate the predicate byte packing

	test := testutils.PredicateTest{
		Config: NewDefaultConfig(subnetEVMUtils.NewUint64(0)),
		ProposerPredicateContext: &precompileconfig.ProposerPredicateContext{
			PrecompilePredicateContext: precompileconfig.PrecompilePredicateContext{
				SnowCtx: snowCtx,
			},
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		StorageSlots: predicateBytes,
		Gas:          GasCostPerSignatureVerification + uint64(len(predicateBytes))*GasCostPerWarpMessageBytes + uint64(numKeys)*GasCostPerWarpSigner,
		GasErr:       errInvalidPredicateBytes,
		PredicateErr: nil, // Won't be reached
	}

	test.Run(t)
}

func TestInvalidWarpMessage(t *testing.T) {
	numKeys := 1
	snowCtx := createSnowCtx([]validatorRange{
		{
			start:  0,
			end:    numKeys,
			weight: 20,
		},
	})
	warpMsg := createWarpMessage(1)
	warpMsgBytes := warpMsg.Bytes()
	warpMsgBytes = append(warpMsgBytes, byte(0x01)) // Invalidate warp message packing
	predicateBytes := predicateutils.PackPredicate(warpMsgBytes)

	test := testutils.PredicateTest{
		Config: NewDefaultConfig(subnetEVMUtils.NewUint64(0)),
		ProposerPredicateContext: &precompileconfig.ProposerPredicateContext{
			PrecompilePredicateContext: precompileconfig.PrecompilePredicateContext{
				SnowCtx: snowCtx,
			},
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		StorageSlots: predicateBytes,
		Gas:          GasCostPerSignatureVerification + uint64(len(predicateBytes))*GasCostPerWarpMessageBytes + uint64(numKeys)*GasCostPerWarpSigner,
		GasErr:       errInvalidWarpMsg,
		PredicateErr: nil, // Won't be reached
	}

	test.Run(t)
}

func TestInvalidAddressedPayload(t *testing.T) {
	numKeys := 1
	snowCtx := createSnowCtx([]validatorRange{
		{
			start:  0,
			end:    numKeys,
			weight: 20,
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
	predicateBytes := predicateutils.PackPredicate(warpMsgBytes)

	test := testutils.PredicateTest{
		Config: NewDefaultConfig(subnetEVMUtils.NewUint64(0)),
		ProposerPredicateContext: &precompileconfig.ProposerPredicateContext{
			PrecompilePredicateContext: precompileconfig.PrecompilePredicateContext{
				SnowCtx: snowCtx,
			},
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		StorageSlots: predicateBytes,
		Gas:          GasCostPerSignatureVerification + uint64(len(predicateBytes))*GasCostPerWarpMessageBytes + uint64(numKeys)*GasCostPerWarpSigner,
		GasErr:       nil,
		PredicateErr: errInvalidAddressedPayload,
	}

	test.Run(t)
}

func TestInvalidBitSet(t *testing.T) {
	unsignedMsg, err := avalancheWarp.NewUnsignedMessage(
		networkID,
		sourceChainID,
		[]byte{1, 2, 3},
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
			start:  0,
			end:    numKeys,
			weight: 20,
		},
	})
	predicateBytes := predicateutils.PackPredicate(msg.Bytes())
	test := testutils.PredicateTest{
		Config: NewDefaultConfig(subnetEVMUtils.NewUint64(0)),
		ProposerPredicateContext: &precompileconfig.ProposerPredicateContext{
			PrecompilePredicateContext: precompileconfig.PrecompilePredicateContext{
				SnowCtx: snowCtx,
			},
			ProposerVMBlockCtx: &block.Context{
				PChainHeight: 1,
			},
		},
		StorageSlots: predicateBytes,
		Gas:          GasCostPerSignatureVerification + uint64(len(predicateBytes))*GasCostPerWarpMessageBytes + uint64(numKeys)*GasCostPerWarpSigner,
		GasErr:       errCannotGetNumSigners,
		PredicateErr: nil, // Won't be reached
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
			predicateBytes       = createPredicate(numSigners)
			expectedPredicateErr error
		)
		// If the number of signers is less than the params.WarpDefaultQuorumNumerator (67)
		if numSigners < int(params.WarpDefaultQuorumNumerator) {
			expectedPredicateErr = avalancheWarp.ErrInsufficientWeight
		}
		if numSigners > int(params.WarpQuorumDenominator) {
			expectedPredicateErr = avalancheWarp.ErrUnknownValidator
		}
		tests[fmt.Sprintf("default quorum %d signature(s)", numSigners)] = testutils.PredicateTest{
			Config: NewDefaultConfig(subnetEVMUtils.NewUint64(0)),
			ProposerPredicateContext: &precompileconfig.ProposerPredicateContext{
				PrecompilePredicateContext: precompileconfig.PrecompilePredicateContext{
					SnowCtx: snowCtx,
				},
				ProposerVMBlockCtx: &block.Context{
					PChainHeight: 1,
				},
			},
			StorageSlots: predicateBytes,
			Gas:          GasCostPerSignatureVerification + uint64(len(predicateBytes))*GasCostPerWarpMessageBytes + uint64(numSigners)*GasCostPerWarpSigner,
			GasErr:       nil,
			PredicateErr: expectedPredicateErr,
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
			predicateBytes       = createPredicate(numSigners)
			expectedPredicateErr error
		)
		// If the number of signers is less than the quorum numerator, expect ErrInsufficientWeight
		if numSigners < nonDefaultQuorumNumerator {
			expectedPredicateErr = avalancheWarp.ErrInsufficientWeight
		}
		if numSigners > int(params.WarpQuorumDenominator) {
			expectedPredicateErr = avalancheWarp.ErrUnknownValidator
		}
		name := fmt.Sprintf("non-default quorum %d signature(s)", numSigners)
		tests[name] = testutils.PredicateTest{
			Config: NewConfig(subnetEVMUtils.NewUint64(0), uint64(nonDefaultQuorumNumerator)),
			ProposerPredicateContext: &precompileconfig.ProposerPredicateContext{
				PrecompilePredicateContext: precompileconfig.PrecompilePredicateContext{
					SnowCtx: snowCtx,
				},
				ProposerVMBlockCtx: &block.Context{
					PChainHeight: 1,
				},
			},
			StorageSlots: predicateBytes,
			Gas:          GasCostPerSignatureVerification + uint64(len(predicateBytes))*GasCostPerWarpMessageBytes + uint64(numSigners)*GasCostPerWarpSigner,
			GasErr:       nil,
			PredicateErr: expectedPredicateErr,
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
