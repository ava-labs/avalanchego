// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/chain"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	_ "embed"

	"github.com/ava-labs/subnet-evm/core"
	"github.com/ava-labs/subnet-evm/eth/tracers"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/params/extras"
	"github.com/ava-labs/subnet-evm/params/paramstest"
	"github.com/ava-labs/subnet-evm/plugin/evm/customheader"
	"github.com/ava-labs/subnet-evm/plugin/evm/extension"
	"github.com/ava-labs/subnet-evm/precompile/contract"
	"github.com/ava-labs/subnet-evm/utils"
	"github.com/ava-labs/subnet-evm/warp"

	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	avagoUtils "github.com/ava-labs/avalanchego/utils"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	warpcontract "github.com/ava-labs/subnet-evm/precompile/contracts/warp"
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
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testSendWarpMessage(t, scheme)
		})
	}
}

func testSendWarpMessage(t *testing.T, scheme string) {
	require := require.New(t)
	genesis := &core.Genesis{}

	require.NoError(genesis.UnmarshalJSON([]byte(toGenesisJSON(paramstest.ForkToChainConfig[upgradetest.Durango]))))
	params.GetExtra(genesis.Config).GenesisPrecompiles = extras.Precompiles{
		warpcontract.ConfigKey: warpcontract.NewDefaultConfig(utils.TimeToNewUint64(upgrade.InitiallyActiveTime)),
	}
	genesisJSON, err := genesis.MarshalJSON()
	require.NoError(err)
	tvm := newVM(t, testVMConfig{
		genesisJSON: string(genesisJSON),
		configJSON:  getConfig(scheme, ""),
	})

	defer func() {
		require.NoError(tvm.vm.Shutdown(t.Context()))
	}()

	acceptedLogsChan := make(chan []*types.Log, 10)
	logsSub := tvm.vm.eth.APIBackend.SubscribeAcceptedLogsEvent(acceptedLogsChan)
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
		tvm.vm.ctx.NetworkID,
		tvm.vm.ctx.ChainID,
		addressedPayload.Bytes(),
	)
	require.NoError(err)

	// Submit a transaction to trigger sending a warp message
	tx0 := types.NewTransaction(uint64(0), warpcontract.ContractAddress, big.NewInt(1), 100_000, big.NewInt(testMinGasPrice), warpSendMessageInput)
	signedTx0, err := types.SignTx(tx0, types.LatestSignerForChainID(tvm.vm.chainConfig.ChainID), testKeys[0].ToECDSA())
	require.NoError(err)

	errs := tvm.vm.txPool.AddRemotesSync([]*types.Transaction{signedTx0})
	require.NoError(errs[0])

	msg, err := tvm.vm.WaitForEvent(t.Context())
	require.NoError(err)
	require.Equal(commonEng.PendingTxs, msg)

	blk, err := tvm.vm.BuildBlock(t.Context())
	require.NoError(err)

	require.NoError(blk.Verify(t.Context()))

	// Verify that the constructed block contains the expected log with an unsigned warp message in the log data
	ethBlock1 := blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock()
	require.Len(ethBlock1.Transactions(), 1)
	receipts := rawdb.ReadReceipts(tvm.vm.chaindb, ethBlock1.Hash(), ethBlock1.NumberU64(), ethBlock1.Time(), tvm.vm.chainConfig)
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
	_, err = tvm.vm.warpBackend.GetMessageSignature(t.Context(), unsignedMessage)
	require.ErrorIs(err, warp.ErrVerifyWarpMessage)
	_, err = tvm.vm.warpBackend.GetBlockSignature(t.Context(), blk.ID())
	require.ErrorIs(err, warp.ErrValidateBlock)

	require.NoError(tvm.vm.SetPreference(t.Context(), blk.ID()))
	require.NoError(blk.Accept(t.Context()))
	tvm.vm.blockChain.DrainAcceptorQueue()

	// Verify the message signature after accepting the block.
	rawSignatureBytes, err := tvm.vm.warpBackend.GetMessageSignature(t.Context(), unsignedMessage)
	require.NoError(err)
	blsSignature, err := bls.SignatureFromBytes(rawSignatureBytes)
	require.NoError(err)

	select {
	case acceptedLogs := <-acceptedLogsChan:
		require.Len(acceptedLogs, 1, "unexpected length of accepted logs")
		require.Equal(acceptedLogs[0], receipts[0].Logs[0])
	case <-time.After(time.Second):
		require.Fail("Failed to read accepted logs from subscription")
	}

	// Verify the produced message signature is valid
	require.True(bls.Verify(tvm.vm.ctx.PublicKey, blsSignature, unsignedMessage.Bytes()))

	// Verify the blockID will now be signed by the backend and produces a valid signature.
	rawSignatureBytes, err = tvm.vm.warpBackend.GetBlockSignature(t.Context(), blk.ID())
	require.NoError(err)
	blsSignature, err = bls.SignatureFromBytes(rawSignatureBytes)
	require.NoError(err)

	blockHashPayload, err := payload.NewHash(blk.ID())
	require.NoError(err)
	unsignedMessage, err = avalancheWarp.NewUnsignedMessage(tvm.vm.ctx.NetworkID, tvm.vm.ctx.ChainID, blockHashPayload.Bytes())
	require.NoError(err)

	// Verify the produced message signature is valid
	require.True(bls.Verify(tvm.vm.ctx.PublicKey, blsSignature, unsignedMessage.Bytes()))
}

func TestValidateWarpMessage(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testValidateWarpMessage(t, scheme)
		})
	}
}

func testValidateWarpMessage(t *testing.T, scheme string) {
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

	testWarpVMTransaction(t, scheme, unsignedMessage, true, exampleWarpPayload)
}

func TestValidateInvalidWarpMessage(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testValidateInvalidWarpMessage(t, scheme)
		})
	}
}

func testValidateInvalidWarpMessage(t *testing.T, scheme string) {
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

	testWarpVMTransaction(t, scheme, unsignedMessage, false, exampleWarpPayload)
}

func TestValidateWarpBlockHash(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testValidateWarpBlockHash(t, scheme)
		})
	}
}

func testValidateWarpBlockHash(t *testing.T, scheme string) {
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

	testWarpVMTransaction(t, scheme, unsignedMessage, true, exampleWarpPayload)
}

func TestValidateInvalidWarpBlockHash(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testValidateInvalidWarpBlockHash(t, scheme)
		})
	}
}

func testValidateInvalidWarpBlockHash(t *testing.T, scheme string) {
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

	testWarpVMTransaction(t, scheme, unsignedMessage, false, exampleWarpPayload)
}

func testWarpVMTransaction(t *testing.T, scheme string, unsignedMessage *avalancheWarp.UnsignedMessage, validSignature bool, txPayload []byte) {
	require := require.New(t)
	genesis := &core.Genesis{}
	require.NoError(genesis.UnmarshalJSON([]byte(toGenesisJSON(paramstest.ForkToChainConfig[upgradetest.Durango]))))
	params.GetExtra(genesis.Config).GenesisPrecompiles = extras.Precompiles{
		warpcontract.ConfigKey: warpcontract.NewDefaultConfig(utils.TimeToNewUint64(upgrade.InitiallyActiveTime)),
	}
	genesisJSON, err := genesis.MarshalJSON()
	require.NoError(err)
	tvm := newVM(t, testVMConfig{
		genesisJSON: string(genesisJSON),
		configJSON:  getConfig(scheme, ""),
	})

	defer func() {
		require.NoError(tvm.vm.Shutdown(t.Context()))
	}()

	acceptedLogsChan := make(chan []*types.Log, 10)
	logsSub := tvm.vm.eth.APIBackend.SubscribeAcceptedLogsEvent(acceptedLogsChan)
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

	tvm.vm.ctx.ValidatorState = &validatorstest.State{
		// TODO: test both Primary Network / C-Chain and non-Primary Network
		GetSubnetIDF: func(context.Context, ids.ID) (ids.ID, error) {
			return ids.Empty, nil
		},
		GetWarpValidatorSetF: func(_ context.Context, height uint64, _ ids.ID) (validators.WarpSet, error) {
			if height < minimumValidPChainHeight {
				return validators.WarpSet{}, getValidatorSetTestErr
			}
			vdrs := validators.WarpSet{
				Validators: []*validators.Warp{
					{
						PublicKey:      blsPublicKey1,
						PublicKeyBytes: bls.PublicKeyToUncompressedBytes(blsPublicKey1),
						Weight:         50,
						NodeIDs:        []ids.NodeID{nodeID1},
					},
					{
						PublicKey:      blsPublicKey2,
						PublicKeyBytes: bls.PublicKeyToUncompressedBytes(blsPublicKey2),
						Weight:         50,
						NodeIDs:        []ids.NodeID{nodeID2},
					},
				},
				TotalWeight: 100,
			}
			avagoUtils.Sort(vdrs.Validators)
			return vdrs, nil
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
		types.LatestSignerForChainID(tvm.vm.chainConfig.ChainID),
		testKeys[0].ToECDSA(),
	)
	require.NoError(err)
	exampleWarpAddress := crypto.CreateAddress(testEthAddrs[0], 0)

	tx, err := types.SignTx(
		types.NewTx(&types.DynamicFeeTx{
			ChainID:   tvm.vm.chainConfig.ChainID,
			Nonce:     1,
			To:        &exampleWarpAddress,
			Gas:       1_000_000,
			GasFeeCap: big.NewInt(225 * utils.GWei),
			GasTipCap: big.NewInt(utils.GWei),
			Value:     common.Big0,
			Data:      txPayload,
			AccessList: types.AccessList{{ // Access list predicate
				Address:     warpcontract.ContractAddress,
				StorageKeys: predicate.New(signedMessage.Bytes()),
			}},
		}),
		types.LatestSignerForChainID(tvm.vm.chainConfig.ChainID),
		testKeys[0].ToECDSA(),
	)
	require.NoError(err)
	errs := tvm.vm.txPool.AddRemotesSync([]*types.Transaction{createTx, tx})
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
	tvm.vm.clock.Set(tvm.vm.clock.Time().Add(2 * time.Second))

	msg, err := tvm.vm.WaitForEvent(t.Context())
	require.NoError(err)
	require.Equal(commonEng.PendingTxs, msg)

	warpBlock, err := tvm.vm.BuildBlockWithContext(t.Context(), blockCtx)
	require.NoError(err)

	warpBlockVerifyWithCtx, ok := warpBlock.(block.WithVerifyContext)
	require.True(ok)
	shouldVerifyWithCtx, err := warpBlockVerifyWithCtx.ShouldVerifyWithContext(t.Context())
	require.NoError(err)
	require.True(shouldVerifyWithCtx)
	require.NoError(warpBlockVerifyWithCtx.VerifyWithContext(t.Context(), blockCtx))
	require.NoError(tvm.vm.SetPreference(t.Context(), warpBlock.ID()))
	require.NoError(warpBlock.Accept(t.Context()))
	tvm.vm.blockChain.DrainAcceptorQueue()

	ethBlock := warpBlock.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock()
	verifiedMessageReceipts := tvm.vm.blockChain.GetReceiptsByHash(ethBlock.Hash())
	require.Len(verifiedMessageReceipts, 2)
	for i, receipt := range verifiedMessageReceipts {
		require.Equal(types.ReceiptStatusSuccessful, receipt.Status, "index: %d", i)
	}

	tracerAPI := tracers.NewAPI(tvm.vm.eth.APIBackend)
	txTraceResults, err := tracerAPI.TraceBlockByHash(t.Context(), ethBlock.Hash(), nil)
	require.NoError(err)
	require.Len(txTraceResults, 2)
	blockTxTraceResultBytes, err := json.Marshal(txTraceResults[1].Result)
	require.NoError(err)
	unmarshalResults := make(map[string]interface{})
	require.NoError(json.Unmarshal(blockTxTraceResultBytes, &unmarshalResults))
	require.Empty(unmarshalResults["returnValue"])

	txTraceResult, err := tracerAPI.TraceTransaction(t.Context(), tx.Hash(), nil)
	require.NoError(err)
	txTraceResultBytes, err := json.Marshal(txTraceResult)
	require.NoError(err)
	require.JSONEq(string(txTraceResultBytes), string(blockTxTraceResultBytes))
}

func TestReceiveWarpMessage(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testReceiveWarpMessageWithScheme(t, scheme)
		})
	}
}

func testReceiveWarpMessageWithScheme(t *testing.T, scheme string) {
	require := require.New(t)
	fork := upgradetest.Durango
	tvm := newVM(t, testVMConfig{
		fork:       &fork,
		configJSON: getConfig(scheme, ""),
	})
	defer func() {
		require.NoError(tvm.vm.Shutdown(t.Context()))
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

	tvm.vm.chainConfigExtra().UpgradeConfig = extras.UpgradeConfig{
		PrecompileUpgrades: []extras.PrecompileUpgrade{
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
			sourceChainID: tvm.vm.ctx.ChainID,
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
			sourceChainID: tvm.vm.ctx.CChainID,
			msgFrom:       fromPrimary,
			useSigners:    signersSubnet,
			blockTime:     upgrade.InitiallyActiveTime.Add(2 * blockGap),
		},
		// Note here we disable warp and re-enable it with RequirePrimaryNetworkSigners
		// by using reEnableTime.
		{
			name:          "subnet message should be signed by subnet with RequirePrimaryNetworkSigners (unimpacted)",
			sourceChainID: tvm.vm.ctx.ChainID,
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
			sourceChainID: tvm.vm.ctx.CChainID,
			msgFrom:       fromPrimary,
			useSigners:    signersPrimary,
			blockTime:     reEnableTime.Add(2 * blockGap),
		},
	}
	// Note each test corresponds to a block, the tests must be ordered by block
	// time and cannot, eg be run in parallel or a separate golang test.
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testReceiveWarpMessage(
				t, tvm.vm, test.sourceChainID, test.msgFrom, test.useSigners, test.blockTime,
			)
		})
	}
}

func testReceiveWarpMessage(
	t *testing.T, vm *VM,
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
		nodeID    ids.NodeID
		secret    bls.Signer
		signature *bls.Signature
		weight    uint64
	}
	weight := uint64(50)
	newSigner := func() signer {
		secret, err := localsigner.New()
		require.NoError(err)
		sig, err := secret.Sign(unsignedMessage.Bytes())
		require.NoError(err)

		return signer{
			nodeID:    ids.GenerateTestNodeID(),
			secret:    secret,
			signature: sig,
			weight:    weight,
		}
	}

	primarySigners := []signer{
		newSigner(),
		newSigner(),
	}
	subnetSigners := []signer{
		newSigner(),
		newSigner(),
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
		GetSubnetIDF: func(context.Context, ids.ID) (ids.ID, error) {
			if msgFrom == fromPrimary {
				return constants.PrimaryNetworkID, nil
			}
			return vm.ctx.SubnetID, nil
		},
		GetWarpValidatorSetF: func(_ context.Context, height uint64, subnetID ids.ID) (validators.WarpSet, error) {
			if height < minimumValidPChainHeight {
				return validators.WarpSet{}, getValidatorSetTestErr
			}
			signers := subnetSigners
			if subnetID == constants.PrimaryNetworkID {
				signers = primarySigners
			}

			vdrs := validators.WarpSet{}
			for _, s := range signers {
				pk := s.secret.PublicKey()
				vdrs.Validators = append(vdrs.Validators, &validators.Warp{
					PublicKey:      pk,
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk),
					Weight:         s.weight,
					NodeIDs:        []ids.NodeID{s.nodeID},
				})
				vdrs.TotalWeight += s.weight
			}
			avagoUtils.Sort(vdrs.Validators)
			return vdrs, nil
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
		types.NewTx(&types.DynamicFeeTx{
			ChainID:   vm.chainConfig.ChainID,
			Nonce:     vm.txPool.Nonce(testEthAddrs[0]),
			To:        &warpcontract.Module.Address,
			Gas:       1_000_000,
			GasFeeCap: big.NewInt(225 * utils.GWei),
			GasTipCap: big.NewInt(utils.GWei),
			Value:     common.Big0,
			Data:      getWarpMsgInput,
			AccessList: types.AccessList{{
				Address:     warpcontract.ContractAddress,
				StorageKeys: predicate.New(signedMessage.Bytes()),
			}},
		}),
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

	msg, err := vm.WaitForEvent(t.Context())
	require.NoError(err)
	require.Equal(commonEng.PendingTxs, msg)

	block2, err := vm.BuildBlockWithContext(t.Context(), validProposerCtx)
	require.NoError(err)

	// Require the block was built with a successful predicate result
	ethBlock := block2.(*chain.BlockWrapper).Block.(extension.ExtendedBlock).GetEthBlock()
	headerPredicateResultsBytes := customheader.PredicateBytesFromExtra(ethBlock.Extra())
	blockResults, err := predicate.ParseBlockResults(headerPredicateResultsBytes)
	require.NoError(err)

	// Predicate results encode the index of invalid warp messages in a bitset.
	txBits := blockResults.Get(getVerifiedWarpMessageTx.Hash(), warpcontract.ContractAddress)
	require.Zero(txBits.Len()) // Empty bitset indicates success

	block2VerifyWithCtx, ok := block2.(block.WithVerifyContext)
	require.True(ok)
	shouldVerifyWithCtx, err := block2VerifyWithCtx.ShouldVerifyWithContext(t.Context())
	require.NoError(err)
	require.True(shouldVerifyWithCtx)
	require.NoError(block2VerifyWithCtx.VerifyWithContext(t.Context(), validProposerCtx))
	require.NoError(vm.SetPreference(t.Context(), block2.ID()))

	// Verify the block with another valid context with identical predicate results
	require.NoError(block2VerifyWithCtx.VerifyWithContext(t.Context(), &block.Context{
		PChainHeight: minimumValidPChainHeight + 1,
	}))

	// Verify the block in a different context causing the warp message to fail verification changing
	// the expected header predicate results.
	require.ErrorIs(block2VerifyWithCtx.VerifyWithContext(t.Context(), &block.Context{
		PChainHeight: minimumValidPChainHeight - 1,
	}), errInvalidHeaderPredicateResults)

	// Accept the block after performing multiple VerifyWithContext operations
	require.NoError(block2.Accept(t.Context()))
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
	txTraceResults, err := tracerAPI.TraceBlockByHash(t.Context(), ethBlock.Hash(), nil)
	require.NoError(err)
	require.Len(txTraceResults, 1)
	blockTxTraceResultBytes, err := json.Marshal(txTraceResults[0].Result)
	require.NoError(err)
	unmarshalResults := make(map[string]interface{})
	require.NoError(json.Unmarshal(blockTxTraceResultBytes, &unmarshalResults))
	require.Equal(common.Bytes2Hex(expectedOutput), unmarshalResults["returnValue"])

	txTraceResult, err := tracerAPI.TraceTransaction(t.Context(), getVerifiedWarpMessageTx.Hash(), nil)
	require.NoError(err)
	txTraceResultBytes, err := json.Marshal(txTraceResult)
	require.NoError(err)
	require.JSONEq(string(txTraceResultBytes), string(blockTxTraceResultBytes))
}

func TestSignatureRequestsToVM(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testSignatureRequestsToVM(t, scheme)
		})
	}
}

func testSignatureRequestsToVM(t *testing.T, scheme string) {
	fork := upgradetest.Durango
	tvm := newVM(t, testVMConfig{
		fork:       &fork,
		configJSON: getConfig(scheme, ""),
	})

	defer func() {
		require.NoError(t, tvm.vm.Shutdown(t.Context()))
	}()

	// Setup known message
	knownPayload, err := payload.NewAddressedCall([]byte{0, 0, 0}, []byte("test"))
	require.NoError(t, err)
	knownWarpMessage, err := avalancheWarp.NewUnsignedMessage(tvm.vm.ctx.NetworkID, tvm.vm.ctx.ChainID, knownPayload.Bytes())
	require.NoError(t, err)

	// Add the known message and get its signature to confirm
	require.NoError(t, tvm.vm.warpBackend.AddMessage(knownWarpMessage))
	knownMessageSignature, err := tvm.vm.warpBackend.GetMessageSignature(t.Context(), knownWarpMessage)
	require.NoError(t, err)

	// Setup known block
	lastAcceptedID, err := tvm.vm.LastAccepted(t.Context())
	require.NoError(t, err)
	knownBlockSignature, err := tvm.vm.warpBackend.GetBlockSignature(t.Context(), lastAcceptedID)
	require.NoError(t, err)

	type testCase struct {
		name             string
		message          *avalancheWarp.UnsignedMessage
		expectedResponse []byte
		err              *commonEng.AppError
	}

	tests := []testCase{
		{
			name:             "known message",
			message:          knownWarpMessage,
			expectedResponse: knownMessageSignature,
		},
		{
			name: "unknown message",
			message: func() *avalancheWarp.UnsignedMessage {
				unknownPayload, err := payload.NewAddressedCall([]byte{1, 1, 1}, []byte("unknown"))
				require.NoError(t, err)
				msg, err := avalancheWarp.NewUnsignedMessage(tvm.vm.ctx.NetworkID, tvm.vm.ctx.ChainID, unknownPayload.Bytes())
				require.NoError(t, err)
				return msg
			}(),
			err: &commonEng.AppError{Code: warp.ParseErrCode},
		},
		{
			name: "known block",
			message: func() *avalancheWarp.UnsignedMessage {
				payload, err := payload.NewHash(lastAcceptedID)
				require.NoError(t, err)
				msg, err := avalancheWarp.NewUnsignedMessage(tvm.vm.ctx.NetworkID, tvm.vm.ctx.ChainID, payload.Bytes())
				require.NoError(t, err)
				return msg
			}(),
			expectedResponse: knownBlockSignature,
		},
		{
			name: "unknown block",
			message: func() *avalancheWarp.UnsignedMessage {
				payload, err := payload.NewHash(ids.GenerateTestID())
				require.NoError(t, err)
				msg, err := avalancheWarp.NewUnsignedMessage(tvm.vm.ctx.NetworkID, tvm.vm.ctx.ChainID, payload.Bytes())
				require.NoError(t, err)
				return msg
			}(),
			err: &commonEng.AppError{Code: warp.VerifyErrCode},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calledSendAppResponseFn := false
			calledSendAppErrorFn := false

			tvm.appSender.SendAppResponseF = func(_ context.Context, _ ids.NodeID, _ uint32, responseBytes []byte) error {
				calledSendAppResponseFn = true
				var response sdk.SignatureResponse
				if err := proto.Unmarshal(responseBytes, &response); err != nil {
					return err
				}
				require.Equal(t, test.expectedResponse, response.Signature)
				return nil
			}

			tvm.appSender.SendAppErrorF = func(context.Context, ids.NodeID, uint32, int32, string) error {
				calledSendAppErrorFn = true
				return nil
			}

			protoMsg := &sdk.SignatureRequest{Message: test.message.Bytes()}
			requestBytes, err := proto.Marshal(protoMsg)
			require.NoError(t, err)
			msg := p2p.PrefixMessage(p2p.ProtocolPrefix(acp118.HandlerID), requestBytes)

			// Send the app request and verify the response
			deadline := time.Now().Add(60 * time.Second)
			appErr := tvm.vm.Network.AppRequest(t.Context(), ids.GenerateTestNodeID(), 1, deadline, msg)
			require.NoError(t, appErr)

			if test.err != nil {
				require.True(t, calledSendAppErrorFn)
			} else {
				require.True(t, calledSendAppResponseFn)
			}
		})
	}
}

func TestClearWarpDB(t *testing.T) {
	ctx, db, genesisBytes := setupGenesis(t, upgradetest.Latest)
	vm := &VM{}
	require.NoError(t, vm.Initialize(t.Context(), ctx, db, genesisBytes, []byte{}, []byte{}, []*commonEng.Fx{}, &enginetest.Sender{}))

	// use multiple messages to test that all messages get cleared
	payloads := [][]byte{[]byte("test1"), []byte("test2"), []byte("test3"), []byte("test4"), []byte("test5")}
	messages := []*avalancheWarp.UnsignedMessage{}

	// add all messages
	for _, payload := range payloads {
		unsignedMsg, err := avalancheWarp.NewUnsignedMessage(vm.ctx.NetworkID, vm.ctx.ChainID, payload)
		require.NoError(t, err)
		require.NoError(t, vm.warpBackend.AddMessage(unsignedMsg))
		// ensure that the message was added
		_, err = vm.warpBackend.GetMessageSignature(t.Context(), unsignedMsg)
		require.NoError(t, err)
		messages = append(messages, unsignedMsg)
	}

	require.NoError(t, vm.Shutdown(t.Context()))

	// Restart VM with the same database default should not prune the warp db
	vm = &VM{}
	// we need new context since the previous one has registered metrics.
	ctx, _, _ = setupGenesis(t, upgradetest.Latest)
	require.NoError(t, vm.Initialize(t.Context(), ctx, db, genesisBytes, []byte{}, []byte{}, []*commonEng.Fx{}, &enginetest.Sender{}))

	// check messages are still present
	for _, message := range messages {
		bytes, err := vm.warpBackend.GetMessageSignature(t.Context(), message)
		require.NoError(t, err)
		require.NotEmpty(t, bytes)
	}

	require.NoError(t, vm.Shutdown(t.Context()))

	// restart the VM with pruning enabled
	vm = &VM{}
	config := `{"prune-warp-db-enabled": true}`
	ctx, _, _ = setupGenesis(t, upgradetest.Latest)
	require.NoError(t, vm.Initialize(t.Context(), ctx, db, genesisBytes, []byte{}, []byte(config), []*commonEng.Fx{}, &enginetest.Sender{}))

	it := vm.warpDB.NewIterator()
	require.False(t, it.Next())
	it.Release()

	// ensure all messages have been deleted
	for _, message := range messages {
		_, err := vm.warpBackend.GetMessageSignature(t.Context(), message)
		require.ErrorIs(t, err, &commonEng.AppError{Code: warp.ParseErrCode})
	}
}
