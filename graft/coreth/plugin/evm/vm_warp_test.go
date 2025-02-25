// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"testing"
	"time"

	_ "embed"

	"github.com/ava-labs/avalanchego/ids"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/upgrade"
	avagoUtils "github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/chain"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/eth/tracers"
	"github.com/ava-labs/coreth/params"
	customheader "github.com/ava-labs/coreth/plugin/evm/header"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap0"
	"github.com/ava-labs/coreth/precompile/contract"
	warpcontract "github.com/ava-labs/coreth/precompile/contracts/warp"
	"github.com/ava-labs/coreth/predicate"
	"github.com/ava-labs/coreth/utils"
	"github.com/ava-labs/coreth/warp"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

var (
	//go:embed ExampleWarp.bin
	exampleWarpBin string
	//go:embed ExampleWarp.abi
	exampleWarpABI string
)

type warpMsgFrom int

const (
	fromSubnet warpMsgFrom = iota
	fromPrimary
)

type useWarpMsgSigners int

const (
	signersSubnet useWarpMsgSigners = iota
	signersPrimary
)

func TestSendWarpMessage(t *testing.T) {
	require := require.New(t)
	issuer, vm, _, _, _ := GenesisVM(t, true, genesisJSONDurango, "", "")

	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
	}()

	acceptedLogsChan := make(chan []*types.Log, 10)
	logsSub := vm.eth.APIBackend.SubscribeAcceptedLogsEvent(acceptedLogsChan)
	defer logsSub.Unsubscribe()

	payloadData := avagoUtils.RandomBytes(100)

	warpSendMessageInput, err := warpcontract.PackSendWarpMessage(payloadData)
	require.NoError(err)
	addressedPayload, err := payload.NewAddressedCall(
		testEthAddrs[0].Bytes(),
		payloadData,
	)
	require.NoError(err)
	expectedUnsignedMessage, err := avalancheWarp.NewUnsignedMessage(
		vm.ctx.NetworkID,
		vm.ctx.ChainID,
		addressedPayload.Bytes(),
	)
	require.NoError(err)

	// Submit a transaction to trigger sending a warp message
	tx0 := types.NewTransaction(uint64(0), warpcontract.ContractAddress, big.NewInt(1), 100_000, big.NewInt(ap0.MinGasPrice), warpSendMessageInput)
	signedTx0, err := types.SignTx(tx0, types.LatestSignerForChainID(vm.chainConfig.ChainID), testKeys[0].ToECDSA())
	require.NoError(err)

	errs := vm.txPool.AddRemotesSync([]*types.Transaction{signedTx0})
	require.NoError(errs[0])

	<-issuer
	blk, err := vm.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(blk.Verify(context.Background()))

	// Verify that the constructed block contains the expected log with an unsigned warp message in the log data
	ethBlock1 := blk.(*chain.BlockWrapper).Block.(*Block).ethBlock
	require.Len(ethBlock1.Transactions(), 1)
	receipts := rawdb.ReadReceipts(vm.chaindb, ethBlock1.Hash(), ethBlock1.NumberU64(), ethBlock1.Time(), vm.chainConfig)
	require.Len(receipts, 1)

	require.Len(receipts[0].Logs, 1)
	expectedTopics := []common.Hash{
		warpcontract.WarpABI.Events["SendWarpMessage"].ID,
		common.BytesToHash(testEthAddrs[0].Bytes()),
		common.Hash(expectedUnsignedMessage.ID()),
	}
	require.Equal(expectedTopics, receipts[0].Logs[0].Topics)
	logData := receipts[0].Logs[0].Data
	unsignedMessage, err := warpcontract.UnpackSendWarpEventDataToMessage(logData)
	require.NoError(err)

	// Verify the signature cannot be fetched before the block is accepted
	_, err = vm.warpBackend.GetMessageSignature(context.TODO(), unsignedMessage)
	require.Error(err)
	_, err = vm.warpBackend.GetBlockSignature(context.TODO(), blk.ID())
	require.Error(err)

	require.NoError(vm.SetPreference(context.Background(), blk.ID()))
	require.NoError(blk.Accept(context.Background()))
	vm.blockChain.DrainAcceptorQueue()

	// Verify the message signature after accepting the block.
	rawSignatureBytes, err := vm.warpBackend.GetMessageSignature(context.TODO(), unsignedMessage)
	require.NoError(err)
	blsSignature, err := bls.SignatureFromBytes(rawSignatureBytes[:])
	require.NoError(err)

	select {
	case acceptedLogs := <-acceptedLogsChan:
		require.Len(acceptedLogs, 1, "unexpected length of accepted logs")
		require.Equal(acceptedLogs[0], receipts[0].Logs[0])
	case <-time.After(time.Second):
		require.Fail("Failed to read accepted logs from subscription")
	}

	// Verify the produced message signature is valid
	require.True(bls.Verify(vm.ctx.PublicKey, blsSignature, unsignedMessage.Bytes()))

	// Verify the blockID will now be signed by the backend and produces a valid signature.
	rawSignatureBytes, err = vm.warpBackend.GetBlockSignature(context.TODO(), blk.ID())
	require.NoError(err)
	blsSignature, err = bls.SignatureFromBytes(rawSignatureBytes[:])
	require.NoError(err)

	blockHashPayload, err := payload.NewHash(blk.ID())
	require.NoError(err)
	unsignedMessage, err = avalancheWarp.NewUnsignedMessage(vm.ctx.NetworkID, vm.ctx.ChainID, blockHashPayload.Bytes())
	require.NoError(err)

	// Verify the produced message signature is valid
	require.True(bls.Verify(vm.ctx.PublicKey, blsSignature, unsignedMessage.Bytes()))
}

func TestValidateWarpMessage(t *testing.T) {
	require := require.New(t)
	sourceChainID := ids.GenerateTestID()
	sourceAddress := common.HexToAddress("0x376c47978271565f56DEB45495afa69E59c16Ab2")
	payloadData := []byte{1, 2, 3}
	addressedPayload, err := payload.NewAddressedCall(
		sourceAddress.Bytes(),
		payloadData,
	)
	require.NoError(err)
	unsignedMessage, err := avalancheWarp.NewUnsignedMessage(testNetworkID, sourceChainID, addressedPayload.Bytes())
	require.NoError(err)

	exampleWarpABI := contract.ParseABI(exampleWarpABI)
	exampleWarpPayload, err := exampleWarpABI.Pack(
		"validateWarpMessage",
		uint32(0),
		sourceChainID,
		sourceAddress,
		payloadData,
	)
	require.NoError(err)

	testWarpVMTransaction(t, unsignedMessage, true, exampleWarpPayload)
}

func TestValidateInvalidWarpMessage(t *testing.T) {
	require := require.New(t)
	sourceChainID := ids.GenerateTestID()
	sourceAddress := common.HexToAddress("0x376c47978271565f56DEB45495afa69E59c16Ab2")
	payloadData := []byte{1, 2, 3}
	addressedPayload, err := payload.NewAddressedCall(
		sourceAddress.Bytes(),
		payloadData,
	)
	require.NoError(err)
	unsignedMessage, err := avalancheWarp.NewUnsignedMessage(testNetworkID, sourceChainID, addressedPayload.Bytes())
	require.NoError(err)

	exampleWarpABI := contract.ParseABI(exampleWarpABI)
	exampleWarpPayload, err := exampleWarpABI.Pack(
		"validateInvalidWarpMessage",
		uint32(0),
	)
	require.NoError(err)

	testWarpVMTransaction(t, unsignedMessage, false, exampleWarpPayload)
}

func TestValidateWarpBlockHash(t *testing.T) {
	require := require.New(t)
	sourceChainID := ids.GenerateTestID()
	blockHash := ids.GenerateTestID()
	blockHashPayload, err := payload.NewHash(blockHash)
	require.NoError(err)
	unsignedMessage, err := avalancheWarp.NewUnsignedMessage(testNetworkID, sourceChainID, blockHashPayload.Bytes())
	require.NoError(err)

	exampleWarpABI := contract.ParseABI(exampleWarpABI)
	exampleWarpPayload, err := exampleWarpABI.Pack(
		"validateWarpBlockHash",
		uint32(0),
		sourceChainID,
		blockHash,
	)
	require.NoError(err)

	testWarpVMTransaction(t, unsignedMessage, true, exampleWarpPayload)
}

func TestValidateInvalidWarpBlockHash(t *testing.T) {
	require := require.New(t)
	sourceChainID := ids.GenerateTestID()
	blockHash := ids.GenerateTestID()
	blockHashPayload, err := payload.NewHash(blockHash)
	require.NoError(err)
	unsignedMessage, err := avalancheWarp.NewUnsignedMessage(testNetworkID, sourceChainID, blockHashPayload.Bytes())
	require.NoError(err)

	exampleWarpABI := contract.ParseABI(exampleWarpABI)
	exampleWarpPayload, err := exampleWarpABI.Pack(
		"validateInvalidWarpBlockHash",
		uint32(0),
	)
	require.NoError(err)

	testWarpVMTransaction(t, unsignedMessage, false, exampleWarpPayload)
}

func testWarpVMTransaction(t *testing.T, unsignedMessage *avalancheWarp.UnsignedMessage, validSignature bool, txPayload []byte) {
	require := require.New(t)
	issuer, vm, _, _, _ := GenesisVM(t, true, genesisJSONDurango, "", "")

	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
	}()

	acceptedLogsChan := make(chan []*types.Log, 10)
	logsSub := vm.eth.APIBackend.SubscribeAcceptedLogsEvent(acceptedLogsChan)
	defer logsSub.Unsubscribe()

	nodeID1 := ids.GenerateTestNodeID()
	blsSecretKey1, err := localsigner.New()
	require.NoError(err)
	blsPublicKey1 := blsSecretKey1.PublicKey()
	blsSignature1, err := blsSecretKey1.Sign(unsignedMessage.Bytes())
	require.NoError(err)

	nodeID2 := ids.GenerateTestNodeID()
	blsSecretKey2, err := localsigner.New()
	require.NoError(err)
	blsPublicKey2 := blsSecretKey2.PublicKey()
	blsSignature2, err := blsSecretKey2.Sign(unsignedMessage.Bytes())
	require.NoError(err)

	blsAggregatedSignature, err := bls.AggregateSignatures([]*bls.Signature{blsSignature1, blsSignature2})
	require.NoError(err)

	minimumValidPChainHeight := uint64(10)
	getValidatorSetTestErr := errors.New("can't get validator set test error")

	vm.ctx.ValidatorState = &validatorstest.State{
		// TODO: test both Primary Network / C-Chain and non-Primary Network
		GetSubnetIDF: func(ctx context.Context, chainID ids.ID) (ids.ID, error) {
			return ids.Empty, nil
		},
		GetValidatorSetF: func(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			if height < minimumValidPChainHeight {
				return nil, getValidatorSetTestErr
			}
			return map[ids.NodeID]*validators.GetValidatorOutput{
				nodeID1: {
					NodeID:    nodeID1,
					PublicKey: blsPublicKey1,
					Weight:    50,
				},
				nodeID2: {
					NodeID:    nodeID2,
					PublicKey: blsPublicKey2,
					Weight:    50,
				},
			}, nil
		},
	}

	signersBitSet := set.NewBits()
	signersBitSet.Add(0)
	signersBitSet.Add(1)

	warpSignature := &avalancheWarp.BitSetSignature{
		Signers: signersBitSet.Bytes(),
	}

	blsAggregatedSignatureBytes := bls.SignatureToBytes(blsAggregatedSignature)
	copy(warpSignature.Signature[:], blsAggregatedSignatureBytes)

	signedMessage, err := avalancheWarp.NewMessage(
		unsignedMessage,
		warpSignature,
	)
	require.NoError(err)

	createTx, err := types.SignTx(
		types.NewContractCreation(0, common.Big0, 7_000_000, big.NewInt(225*utils.GWei), common.Hex2Bytes(exampleWarpBin)),
		types.LatestSignerForChainID(vm.chainConfig.ChainID),
		testKeys[0].ToECDSA(),
	)
	require.NoError(err)
	exampleWarpAddress := crypto.CreateAddress(testEthAddrs[0], 0)

	tx, err := types.SignTx(
		predicate.NewPredicateTx(
			vm.chainConfig.ChainID,
			1,
			&exampleWarpAddress,
			1_000_000,
			big.NewInt(225*utils.GWei),
			big.NewInt(utils.GWei),
			common.Big0,
			txPayload,
			types.AccessList{},
			warpcontract.ContractAddress,
			signedMessage.Bytes(),
		),
		types.LatestSignerForChainID(vm.chainConfig.ChainID),
		testKeys[0].ToECDSA(),
	)
	require.NoError(err)
	errs := vm.txPool.AddRemotesSync([]*types.Transaction{createTx, tx})
	for i, err := range errs {
		require.NoError(err, "failed to add tx at index %d", i)
	}

	// If [validSignature] set the signature to be considered valid at the verified height.
	blockCtx := &block.Context{
		PChainHeight: minimumValidPChainHeight - 1,
	}
	if validSignature {
		blockCtx.PChainHeight = minimumValidPChainHeight
	}
	vm.clock.Set(vm.clock.Time().Add(2 * time.Second))
	<-issuer

	warpBlock, err := vm.BuildBlockWithContext(context.Background(), blockCtx)
	require.NoError(err)

	warpBlockVerifyWithCtx, ok := warpBlock.(block.WithVerifyContext)
	require.True(ok)
	shouldVerifyWithCtx, err := warpBlockVerifyWithCtx.ShouldVerifyWithContext(context.Background())
	require.NoError(err)
	require.True(shouldVerifyWithCtx)
	require.NoError(warpBlockVerifyWithCtx.VerifyWithContext(context.Background(), blockCtx))
	require.NoError(vm.SetPreference(context.Background(), warpBlock.ID()))
	require.NoError(warpBlock.Accept(context.Background()))
	vm.blockChain.DrainAcceptorQueue()

	ethBlock := warpBlock.(*chain.BlockWrapper).Block.(*Block).ethBlock
	verifiedMessageReceipts := vm.blockChain.GetReceiptsByHash(ethBlock.Hash())
	require.Len(verifiedMessageReceipts, 2)
	for i, receipt := range verifiedMessageReceipts {
		require.Equal(types.ReceiptStatusSuccessful, receipt.Status, "index: %d", i)
	}

	tracerAPI := tracers.NewAPI(vm.eth.APIBackend)
	txTraceResults, err := tracerAPI.TraceBlockByHash(context.Background(), ethBlock.Hash(), nil)
	require.NoError(err)
	require.Len(txTraceResults, 2)
	blockTxTraceResultBytes, err := json.Marshal(txTraceResults[1].Result)
	require.NoError(err)
	unmarshalResults := make(map[string]interface{})
	require.NoError(json.Unmarshal(blockTxTraceResultBytes, &unmarshalResults))
	require.Equal("", unmarshalResults["returnValue"])

	txTraceResult, err := tracerAPI.TraceTransaction(context.Background(), tx.Hash(), nil)
	require.NoError(err)
	txTraceResultBytes, err := json.Marshal(txTraceResult)
	require.NoError(err)
	require.JSONEq(string(txTraceResultBytes), string(blockTxTraceResultBytes))
}

func TestReceiveWarpMessage(t *testing.T) {
	require := require.New(t)
	issuer, vm, _, _, _ := GenesisVM(t, true, genesisJSONDurango, "", "")

	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
	}()

	// enable warp at the default genesis time
	enableTime := upgrade.InitiallyActiveTime
	enableConfig := warpcontract.NewDefaultConfig(utils.TimeToNewUint64(enableTime))

	// disable warp so we can re-enable it with RequirePrimaryNetworkSigners
	disableTime := upgrade.InitiallyActiveTime.Add(10 * time.Second)
	disableConfig := warpcontract.NewDisableConfig(utils.TimeToNewUint64(disableTime))

	// re-enable warp with RequirePrimaryNetworkSigners
	reEnableTime := disableTime.Add(10 * time.Second)
	reEnableConfig := warpcontract.NewConfig(
		utils.TimeToNewUint64(reEnableTime),
		0,    // QuorumNumerator
		true, // RequirePrimaryNetworkSigners
	)

	vm.chainConfig.UpgradeConfig = params.UpgradeConfig{
		PrecompileUpgrades: []params.PrecompileUpgrade{
			{Config: enableConfig},
			{Config: disableConfig},
			{Config: reEnableConfig},
		},
	}

	type test struct {
		name          string
		sourceChainID ids.ID
		msgFrom       warpMsgFrom
		useSigners    useWarpMsgSigners
		blockTime     time.Time
	}

	blockGap := 2 * time.Second // Build blocks with a gap. Blocks built too quickly will have high fees.
	tests := []test{
		{
			name:          "subnet message should be signed by subnet without RequirePrimaryNetworkSigners",
			sourceChainID: vm.ctx.ChainID,
			msgFrom:       fromSubnet,
			useSigners:    signersSubnet,
			blockTime:     upgrade.InitiallyActiveTime,
		},
		{
			name:          "P-Chain message should be signed by subnet without RequirePrimaryNetworkSigners",
			sourceChainID: constants.PlatformChainID,
			msgFrom:       fromPrimary,
			useSigners:    signersSubnet,
			blockTime:     upgrade.InitiallyActiveTime.Add(blockGap),
		},
		{
			name:          "C-Chain message should be signed by subnet without RequirePrimaryNetworkSigners",
			sourceChainID: vm.ctx.CChainID,
			msgFrom:       fromPrimary,
			useSigners:    signersSubnet,
			blockTime:     upgrade.InitiallyActiveTime.Add(2 * blockGap),
		},
		// Note here we disable warp and re-enable it with RequirePrimaryNetworkSigners
		// by using reEnableTime.
		{
			name:          "subnet message should be signed by subnet with RequirePrimaryNetworkSigners (unimpacted)",
			sourceChainID: vm.ctx.ChainID,
			msgFrom:       fromSubnet,
			useSigners:    signersSubnet,
			blockTime:     reEnableTime,
		},
		{
			name:          "P-Chain message should be signed by subnet with RequirePrimaryNetworkSigners (unimpacted)",
			sourceChainID: constants.PlatformChainID,
			msgFrom:       fromPrimary,
			useSigners:    signersSubnet,
			blockTime:     reEnableTime.Add(blockGap),
		},
		{
			name:          "C-Chain message should be signed by primary with RequirePrimaryNetworkSigners (impacted)",
			sourceChainID: vm.ctx.CChainID,
			msgFrom:       fromPrimary,
			useSigners:    signersPrimary,
			blockTime:     reEnableTime.Add(2 * blockGap),
		},
	}
	// Note each test corresponds to a block, the tests must be ordered by block
	// time and cannot, eg be run in parallel or a separate golang test.
	for _, test := range tests {
		testReceiveWarpMessage(
			t, issuer, vm, test.sourceChainID, test.msgFrom, test.useSigners, test.blockTime,
		)
	}
}

func testReceiveWarpMessage(
	t *testing.T, issuer chan commonEng.Message, vm *VM,
	sourceChainID ids.ID,
	msgFrom warpMsgFrom, useSigners useWarpMsgSigners,
	blockTime time.Time,
) {
	require := require.New(t)
	payloadData := avagoUtils.RandomBytes(100)
	addressedPayload, err := payload.NewAddressedCall(
		testEthAddrs[0].Bytes(),
		payloadData,
	)
	require.NoError(err)

	vm.ctx.SubnetID = ids.GenerateTestID()
	vm.ctx.NetworkID = testNetworkID
	unsignedMessage, err := avalancheWarp.NewUnsignedMessage(
		vm.ctx.NetworkID,
		sourceChainID,
		addressedPayload.Bytes(),
	)
	require.NoError(err)

	type signer struct {
		networkID ids.ID
		nodeID    ids.NodeID
		secret    bls.Signer
		signature *bls.Signature
		weight    uint64
	}
	newSigner := func(networkID ids.ID, weight uint64) signer {
		secret, err := localsigner.New()
		require.NoError(err)
		sig, err := secret.Sign(unsignedMessage.Bytes())
		require.NoError(err)

		return signer{
			networkID: networkID,
			nodeID:    ids.GenerateTestNodeID(),
			secret:    secret,
			signature: sig,
			weight:    weight,
		}
	}

	primarySigners := []signer{
		newSigner(constants.PrimaryNetworkID, 50),
		newSigner(constants.PrimaryNetworkID, 50),
	}
	subnetSigners := []signer{
		newSigner(vm.ctx.SubnetID, 50),
		newSigner(vm.ctx.SubnetID, 50),
	}
	signers := subnetSigners
	if useSigners == signersPrimary {
		signers = primarySigners
	}

	blsSignatures := make([]*bls.Signature, len(signers))
	for i := range signers {
		blsSignatures[i] = signers[i].signature
	}
	blsAggregatedSignature, err := bls.AggregateSignatures(blsSignatures)
	require.NoError(err)

	minimumValidPChainHeight := uint64(10)
	getValidatorSetTestErr := errors.New("can't get validator set test error")

	vm.ctx.ValidatorState = &validatorstest.State{
		GetSubnetIDF: func(ctx context.Context, chainID ids.ID) (ids.ID, error) {
			if msgFrom == fromPrimary {
				return constants.PrimaryNetworkID, nil
			}
			return vm.ctx.SubnetID, nil
		},
		GetValidatorSetF: func(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			if height < minimumValidPChainHeight {
				return nil, getValidatorSetTestErr
			}
			signers := subnetSigners
			if subnetID == constants.PrimaryNetworkID {
				signers = primarySigners
			}

			vdrOutput := make(map[ids.NodeID]*validators.GetValidatorOutput)
			for _, s := range signers {
				vdrOutput[s.nodeID] = &validators.GetValidatorOutput{
					NodeID:    s.nodeID,
					PublicKey: s.secret.PublicKey(),
					Weight:    s.weight,
				}
			}
			return vdrOutput, nil
		},
	}

	signersBitSet := set.NewBits()
	for i := range signers {
		signersBitSet.Add(i)
	}

	warpSignature := &avalancheWarp.BitSetSignature{
		Signers: signersBitSet.Bytes(),
	}

	blsAggregatedSignatureBytes := bls.SignatureToBytes(blsAggregatedSignature)
	copy(warpSignature.Signature[:], blsAggregatedSignatureBytes)

	signedMessage, err := avalancheWarp.NewMessage(
		unsignedMessage,
		warpSignature,
	)
	require.NoError(err)

	getWarpMsgInput, err := warpcontract.PackGetVerifiedWarpMessage(0)
	require.NoError(err)
	getVerifiedWarpMessageTx, err := types.SignTx(
		predicate.NewPredicateTx(
			vm.chainConfig.ChainID,
			vm.txPool.Nonce(testEthAddrs[0]),
			&warpcontract.Module.Address,
			1_000_000,
			big.NewInt(225*utils.GWei),
			big.NewInt(utils.GWei),
			common.Big0,
			getWarpMsgInput,
			types.AccessList{},
			warpcontract.ContractAddress,
			signedMessage.Bytes(),
		),
		types.LatestSignerForChainID(vm.chainConfig.ChainID),
		testKeys[0].ToECDSA(),
	)
	require.NoError(err)
	errs := vm.txPool.AddRemotesSync([]*types.Transaction{getVerifiedWarpMessageTx})
	for i, err := range errs {
		require.NoError(err, "failed to add tx at index %d", i)
	}

	// Build, verify, and accept block with valid proposer context.
	validProposerCtx := &block.Context{
		PChainHeight: minimumValidPChainHeight,
	}
	vm.clock.Set(blockTime)
	<-issuer

	block2, err := vm.BuildBlockWithContext(context.Background(), validProposerCtx)
	require.NoError(err)

	// Require the block was built with a successful predicate result
	ethBlock := block2.(*chain.BlockWrapper).Block.(*Block).ethBlock
	headerPredicateResultsBytes := customheader.PredicateBytesFromExtra(ethBlock.Extra())
	results, err := predicate.ParseResults(headerPredicateResultsBytes)
	require.NoError(err)

	// Predicate results encode the index of invalid warp messages in a bitset.
	// An empty bitset indicates success.
	txResultsBytes := results.GetResults(
		getVerifiedWarpMessageTx.Hash(),
		warpcontract.ContractAddress,
	)
	bitset := set.BitsFromBytes(txResultsBytes)
	require.Zero(bitset.Len()) // Empty bitset indicates success

	block2VerifyWithCtx, ok := block2.(block.WithVerifyContext)
	require.True(ok)
	shouldVerifyWithCtx, err := block2VerifyWithCtx.ShouldVerifyWithContext(context.Background())
	require.NoError(err)
	require.True(shouldVerifyWithCtx)
	require.NoError(block2VerifyWithCtx.VerifyWithContext(context.Background(), validProposerCtx))
	require.NoError(vm.SetPreference(context.Background(), block2.ID()))

	// Verify the block with another valid context with identical predicate results
	require.NoError(block2VerifyWithCtx.VerifyWithContext(context.Background(), &block.Context{
		PChainHeight: minimumValidPChainHeight + 1,
	}))

	// Verify the block in a different context causing the warp message to fail verification changing
	// the expected header predicate results.
	require.ErrorIs(block2VerifyWithCtx.VerifyWithContext(context.Background(), &block.Context{
		PChainHeight: minimumValidPChainHeight - 1,
	}), errInvalidHeaderPredicateResults)

	// Accept the block after performing multiple VerifyWithContext operations
	require.NoError(block2.Accept(context.Background()))
	vm.blockChain.DrainAcceptorQueue()

	verifiedMessageReceipts := vm.blockChain.GetReceiptsByHash(ethBlock.Hash())
	require.Len(verifiedMessageReceipts, 1)
	verifiedMessageTxReceipt := verifiedMessageReceipts[0]
	require.Equal(types.ReceiptStatusSuccessful, verifiedMessageTxReceipt.Status)

	expectedOutput, err := warpcontract.PackGetVerifiedWarpMessageOutput(warpcontract.GetVerifiedWarpMessageOutput{
		Message: warpcontract.WarpMessage{
			SourceChainID:       common.Hash(sourceChainID),
			OriginSenderAddress: testEthAddrs[0],
			Payload:             payloadData,
		},
		Valid: true,
	})
	require.NoError(err)

	tracerAPI := tracers.NewAPI(vm.eth.APIBackend)
	txTraceResults, err := tracerAPI.TraceBlockByHash(context.Background(), ethBlock.Hash(), nil)
	require.NoError(err)
	require.Len(txTraceResults, 1)
	blockTxTraceResultBytes, err := json.Marshal(txTraceResults[0].Result)
	require.NoError(err)
	unmarshalResults := make(map[string]interface{})
	require.NoError(json.Unmarshal(blockTxTraceResultBytes, &unmarshalResults))
	require.Equal(common.Bytes2Hex(expectedOutput), unmarshalResults["returnValue"])

	txTraceResult, err := tracerAPI.TraceTransaction(context.Background(), getVerifiedWarpMessageTx.Hash(), nil)
	require.NoError(err)
	txTraceResultBytes, err := json.Marshal(txTraceResult)
	require.NoError(err)
	require.JSONEq(string(txTraceResultBytes), string(blockTxTraceResultBytes))
}

func TestMessageSignatureRequestsToVM(t *testing.T) {
	_, vm, _, _, appSender := GenesisVM(t, true, genesisJSONDurango, "", "")

	defer func() {
		err := vm.Shutdown(context.Background())
		require.NoError(t, err)
	}()

	// Generate a new warp unsigned message and add to warp backend
	warpMessage, err := avalancheWarp.NewUnsignedMessage(vm.ctx.NetworkID, vm.ctx.ChainID, []byte{1, 2, 3})
	require.NoError(t, err)

	// Add the known message and get its signature to confirm.
	err = vm.warpBackend.AddMessage(warpMessage)
	require.NoError(t, err)
	signature, err := vm.warpBackend.GetMessageSignature(context.TODO(), warpMessage)
	require.NoError(t, err)
	var knownSignature [bls.SignatureLen]byte
	copy(knownSignature[:], signature)

	tests := map[string]struct {
		messageID        ids.ID
		expectedResponse [bls.SignatureLen]byte
	}{
		"known": {
			messageID:        warpMessage.ID(),
			expectedResponse: knownSignature,
		},
		"unknown": {
			messageID:        ids.GenerateTestID(),
			expectedResponse: [bls.SignatureLen]byte{},
		},
	}

	for name, test := range tests {
		calledSendAppResponseFn := false
		appSender.SendAppResponseF = func(ctx context.Context, nodeID ids.NodeID, requestID uint32, responseBytes []byte) error {
			calledSendAppResponseFn = true
			var response message.SignatureResponse
			_, err := message.Codec.Unmarshal(responseBytes, &response)
			require.NoError(t, err)
			require.Equal(t, test.expectedResponse, response.Signature)

			return nil
		}
		t.Run(name, func(t *testing.T) {
			var signatureRequest message.Request = message.MessageSignatureRequest{
				MessageID: test.messageID,
			}

			requestBytes, err := message.Codec.Marshal(message.Version, &signatureRequest)
			require.NoError(t, err)

			// Send the app request and make sure we called SendAppResponseFn
			deadline := time.Now().Add(60 * time.Second)
			err = vm.Network.AppRequest(context.Background(), ids.GenerateTestNodeID(), 1, deadline, requestBytes)
			require.NoError(t, err)
			require.True(t, calledSendAppResponseFn)
		})
	}
}

func TestBlockSignatureRequestsToVM(t *testing.T) {
	_, vm, _, _, appSender := GenesisVM(t, true, genesisJSONDurango, "", "")

	defer func() {
		err := vm.Shutdown(context.Background())
		require.NoError(t, err)
	}()

	lastAcceptedID, err := vm.LastAccepted(context.Background())
	require.NoError(t, err)

	signature, err := vm.warpBackend.GetBlockSignature(context.TODO(), lastAcceptedID)
	require.NoError(t, err)
	var knownSignature [bls.SignatureLen]byte
	copy(knownSignature[:], signature)

	tests := map[string]struct {
		blockID          ids.ID
		expectedResponse [bls.SignatureLen]byte
	}{
		"known": {
			blockID:          lastAcceptedID,
			expectedResponse: knownSignature,
		},
		"unknown": {
			blockID:          ids.GenerateTestID(),
			expectedResponse: [bls.SignatureLen]byte{},
		},
	}

	for name, test := range tests {
		calledSendAppResponseFn := false
		appSender.SendAppResponseF = func(ctx context.Context, nodeID ids.NodeID, requestID uint32, responseBytes []byte) error {
			calledSendAppResponseFn = true
			var response message.SignatureResponse
			_, err := message.Codec.Unmarshal(responseBytes, &response)
			require.NoError(t, err)
			require.Equal(t, test.expectedResponse, response.Signature)

			return nil
		}
		t.Run(name, func(t *testing.T) {
			var signatureRequest message.Request = message.BlockSignatureRequest{
				BlockID: test.blockID,
			}

			requestBytes, err := message.Codec.Marshal(message.Version, &signatureRequest)
			require.NoError(t, err)

			// Send the app request and make sure we called SendAppResponseFn
			deadline := time.Now().Add(60 * time.Second)
			err = vm.Network.AppRequest(context.Background(), ids.GenerateTestNodeID(), 1, deadline, requestBytes)
			require.NoError(t, err)
			require.True(t, calledSendAppResponseFn)
		})
	}
}

func TestClearWarpDB(t *testing.T) {
	ctx, db, genesisBytes, issuer, _ := setupGenesis(t, genesisJSONLatest)
	vm := &VM{}
	err := vm.Initialize(context.Background(), ctx, db, genesisBytes, []byte{}, []byte{}, issuer, []*commonEng.Fx{}, &enginetest.Sender{})
	require.NoError(t, err)

	// use multiple messages to test that all messages get cleared
	payloads := [][]byte{[]byte("test1"), []byte("test2"), []byte("test3"), []byte("test4"), []byte("test5")}
	messages := []*avalancheWarp.UnsignedMessage{}

	// add all messages
	for _, payload := range payloads {
		unsignedMsg, err := avalancheWarp.NewUnsignedMessage(vm.ctx.NetworkID, vm.ctx.ChainID, payload)
		require.NoError(t, err)
		err = vm.warpBackend.AddMessage(unsignedMsg)
		require.NoError(t, err)
		// ensure that the message was added
		_, err = vm.warpBackend.GetMessageSignature(context.TODO(), unsignedMsg)
		require.NoError(t, err)
		messages = append(messages, unsignedMsg)
	}

	require.NoError(t, vm.Shutdown(context.Background()))

	// Restart VM with the same database default should not prune the warp db
	vm = &VM{}
	// we need new context since the previous one has registered metrics.
	ctx, _, _, _, _ = setupGenesis(t, genesisJSONLatest)
	err = vm.Initialize(context.Background(), ctx, db, genesisBytes, []byte{}, []byte{}, issuer, []*commonEng.Fx{}, &enginetest.Sender{})
	require.NoError(t, err)

	// check messages are still present
	for _, message := range messages {
		bytes, err := vm.warpBackend.GetMessageSignature(context.TODO(), message)
		require.NoError(t, err)
		require.NotEmpty(t, bytes)
	}

	require.NoError(t, vm.Shutdown(context.Background()))

	// restart the VM with pruning enabled
	vm = &VM{}
	config := `{"prune-warp-db-enabled": true}`
	ctx, _, _, _, _ = setupGenesis(t, genesisJSONLatest)
	err = vm.Initialize(context.Background(), ctx, db, genesisBytes, []byte{}, []byte(config), issuer, []*commonEng.Fx{}, &enginetest.Sender{})
	require.NoError(t, err)

	it := vm.warpDB.NewIterator()
	require.False(t, it.Next())
	it.Release()

	// ensure all messages have been deleted
	for _, message := range messages {
		_, err := vm.warpBackend.GetMessageSignature(context.TODO(), message)
		require.ErrorIs(t, err, &commonEng.AppError{Code: warp.ParseErrCode})
	}
}
