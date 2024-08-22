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
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/upgrade"
	avagoUtils "github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/chain"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/subnet-evm/core"
	"github.com/ava-labs/subnet-evm/core/rawdb"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/eth/tracers"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/plugin/evm/message"
	"github.com/ava-labs/subnet-evm/precompile/contract"
	"github.com/ava-labs/subnet-evm/precompile/contracts/warp"
	"github.com/ava-labs/subnet-evm/predicate"
	"github.com/ava-labs/subnet-evm/utils"
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
	genesis := &core.Genesis{}
	require.NoError(genesis.UnmarshalJSON([]byte(genesisJSONDurango)))
	genesis.Config.GenesisPrecompiles = params.Precompiles{
		warp.ConfigKey: warp.NewDefaultConfig(utils.TimeToNewUint64(upgrade.InitiallyActiveTime)),
	}
	genesisJSON, err := genesis.MarshalJSON()
	require.NoError(err)
	issuer, vm, _, _ := GenesisVM(t, true, string(genesisJSON), "", "")

	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
	}()

	acceptedLogsChan := make(chan []*types.Log, 10)
	logsSub := vm.eth.APIBackend.SubscribeAcceptedLogsEvent(acceptedLogsChan)
	defer logsSub.Unsubscribe()

	payloadData := avagoUtils.RandomBytes(100)

	warpSendMessageInput, err := warp.PackSendWarpMessage(payloadData)
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
	tx0 := types.NewTransaction(uint64(0), warp.ContractAddress, big.NewInt(1), 100_000, big.NewInt(testMinGasPrice), warpSendMessageInput)
	signedTx0, err := types.SignTx(tx0, types.LatestSignerForChainID(vm.chainConfig.ChainID), testKeys[0])
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
		warp.WarpABI.Events["SendWarpMessage"].ID,
		common.BytesToHash(testEthAddrs[0].Bytes()),
		common.Hash(expectedUnsignedMessage.ID()),
	}
	require.Equal(expectedTopics, receipts[0].Logs[0].Topics)
	logData := receipts[0].Logs[0].Data
	unsignedMessage, err := warp.UnpackSendWarpEventDataToMessage(logData)
	require.NoError(err)
	unsignedMessageID := unsignedMessage.ID()

	// Verify the signature cannot be fetched before the block is accepted
	_, err = vm.warpBackend.GetMessageSignature(unsignedMessageID)
	require.Error(err)
	_, err = vm.warpBackend.GetBlockSignature(blk.ID())
	require.Error(err)

	require.NoError(vm.SetPreference(context.Background(), blk.ID()))
	require.NoError(blk.Accept(context.Background()))
	vm.blockChain.DrainAcceptorQueue()

	// Verify the message signature after accepting the block.
	rawSignatureBytes, err := vm.warpBackend.GetMessageSignature(unsignedMessageID)
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
	rawSignatureBytes, err = vm.warpBackend.GetBlockSignature(blk.ID())
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
	genesis := &core.Genesis{}
	require.NoError(genesis.UnmarshalJSON([]byte(genesisJSONDurango)))
	genesis.Config.GenesisPrecompiles = params.Precompiles{
		warp.ConfigKey: warp.NewDefaultConfig(utils.TimeToNewUint64(upgrade.InitiallyActiveTime)),
	}
	genesisJSON, err := genesis.MarshalJSON()
	require.NoError(err)
	issuer, vm, _, _ := GenesisVM(t, true, string(genesisJSON), "", "")

	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
	}()

	acceptedLogsChan := make(chan []*types.Log, 10)
	logsSub := vm.eth.APIBackend.SubscribeAcceptedLogsEvent(acceptedLogsChan)
	defer logsSub.Unsubscribe()

	nodeID1 := ids.GenerateTestNodeID()
	blsSecretKey1, err := bls.NewSecretKey()
	require.NoError(err)
	blsPublicKey1 := bls.PublicFromSecretKey(blsSecretKey1)
	blsSignature1 := bls.Sign(blsSecretKey1, unsignedMessage.Bytes())

	nodeID2 := ids.GenerateTestNodeID()
	blsSecretKey2, err := bls.NewSecretKey()
	require.NoError(err)
	blsPublicKey2 := bls.PublicFromSecretKey(blsSecretKey2)
	blsSignature2 := bls.Sign(blsSecretKey2, unsignedMessage.Bytes())

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
		types.NewContractCreation(0, common.Big0, 7_000_000, big.NewInt(225*params.GWei), common.Hex2Bytes(exampleWarpBin)),
		types.LatestSignerForChainID(vm.chainConfig.ChainID),
		testKeys[0],
	)
	require.NoError(err)
	exampleWarpAddress := crypto.CreateAddress(testEthAddrs[0], 0)

	tx, err := types.SignTx(
		predicate.NewPredicateTx(
			vm.chainConfig.ChainID,
			1,
			&exampleWarpAddress,
			1_000_000,
			big.NewInt(225*params.GWei),
			big.NewInt(params.GWei),
			common.Big0,
			txPayload,
			types.AccessList{},
			warp.ContractAddress,
			signedMessage.Bytes(),
		),
		types.LatestSignerForChainID(vm.chainConfig.ChainID),
		testKeys[0],
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
	genesis := &core.Genesis{}
	require.NoError(genesis.UnmarshalJSON([]byte(genesisJSONDurango)))
	genesis.Config.GenesisPrecompiles = params.Precompiles{
		// Note that warp is enabled without RequirePrimaryNetworkSigners
		// by default in the genesis configuration.
		warp.ConfigKey: warp.NewDefaultConfig(utils.TimeToNewUint64(upgrade.InitiallyActiveTime)),
	}
	genesisJSON, err := genesis.MarshalJSON()
	require.NoError(err)

	// disable warp so we can re-enable it with RequirePrimaryNetworkSigners
	disableTime := upgrade.InitiallyActiveTime.Add(10 * time.Second)
	disableConfig := warp.NewDisableConfig(utils.TimeToNewUint64(disableTime))

	// re-enable warp with RequirePrimaryNetworkSigners
	reEnableTime := disableTime.Add(10 * time.Second)
	reEnableConfig := warp.NewConfig(
		utils.TimeToNewUint64(reEnableTime),
		0,    // QuorumNumerator
		true, // RequirePrimaryNetworkSigners
	)

	upgradeConfig := params.UpgradeConfig{
		PrecompileUpgrades: []params.PrecompileUpgrade{
			{Config: disableConfig},
			{Config: reEnableConfig},
		},
	}
	upgradeBytes, err := json.Marshal(upgradeConfig)
	require.NoError(err)

	issuer, vm, _, _ := GenesisVM(t, true, string(genesisJSON), "", string(upgradeBytes))

	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
	}()

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
			sourceChainID: testCChainID,
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
			sourceChainID: testCChainID,
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
		secret    *bls.SecretKey
		signature *bls.Signature
		weight    uint64
	}
	newSigner := func(networkID ids.ID, weight uint64) signer {
		secret, err := bls.NewSecretKey()
		require.NoError(err)
		return signer{
			networkID: networkID,
			nodeID:    ids.GenerateTestNodeID(),
			secret:    secret,
			signature: bls.Sign(secret, unsignedMessage.Bytes()),
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
					PublicKey: bls.PublicFromSecretKey(s.secret),
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

	getWarpMsgInput, err := warp.PackGetVerifiedWarpMessage(0)
	require.NoError(err)
	getVerifiedWarpMessageTx, err := types.SignTx(
		predicate.NewPredicateTx(
			vm.chainConfig.ChainID,
			vm.txPool.Nonce(testEthAddrs[0]),
			&warp.Module.Address,
			1_000_000,
			big.NewInt(225*params.GWei),
			big.NewInt(params.GWei),
			common.Big0,
			getWarpMsgInput,
			types.AccessList{},
			warp.ContractAddress,
			signedMessage.Bytes(),
		),
		types.LatestSignerForChainID(vm.chainConfig.ChainID),
		testKeys[0],
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
	headerPredicateResultsBytes, ok := predicate.GetPredicateResultBytes(ethBlock.Extra())
	require.True(ok)
	results, err := predicate.ParseResults(headerPredicateResultsBytes)
	require.NoError(err)

	// Predicate results encode the index of invalid warp messages in a bitset.
	// An empty bitset indicates success.
	txResultsBytes := results.GetResults(
		getVerifiedWarpMessageTx.Hash(),
		warp.ContractAddress,
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

	expectedOutput, err := warp.PackGetVerifiedWarpMessageOutput(warp.GetVerifiedWarpMessageOutput{
		Message: warp.WarpMessage{
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
	_, vm, _, appSender := GenesisVM(t, true, genesisJSONSubnetEVM, "", "")

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
	signature, err := vm.warpBackend.GetMessageSignature(warpMessage.ID())
	require.NoError(t, err)

	tests := map[string]struct {
		messageID        ids.ID
		expectedResponse [bls.SignatureLen]byte
	}{
		"known": {
			messageID:        warpMessage.ID(),
			expectedResponse: signature,
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
	_, vm, _, appSender := GenesisVM(t, true, genesisJSONSubnetEVM, "", "")

	defer func() {
		err := vm.Shutdown(context.Background())
		require.NoError(t, err)
	}()

	lastAcceptedID, err := vm.LastAccepted(context.Background())
	require.NoError(t, err)

	signature, err := vm.warpBackend.GetBlockSignature(lastAcceptedID)
	require.NoError(t, err)

	tests := map[string]struct {
		blockID          ids.ID
		expectedResponse [bls.SignatureLen]byte
	}{
		"known": {
			blockID:          lastAcceptedID,
			expectedResponse: signature,
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
