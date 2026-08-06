package main

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log"
	"math/big"
	"strings"

	"github.com/ava-labs/libevm"
	"github.com/ava-labs/libevm/accounts/abi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethclient"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/sidecar/internal/relayer"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// fetchCChainSend reads a Teleporter send transaction on the C-Chain, or on
// the --source-rpc chain. It returns the unsigned warp message, from the
// SendWarpMessage log of the precompile, and the TeleporterMessageV2 struct,
// from the SendCrossChainMessage log of the Teleporter.
func fetchCChainSend(
	ctx context.Context,
	avalancheURI, txHash string,
	teleporter common.Address,
) (*avalancheWarp.UnsignedMessage, *relayer.TeleporterMessageV2, error) {
	rpcURL := sourceRPCOverride
	if rpcURL == "" {
		rpcURL = strings.TrimSuffix(avalancheURI, "/") + "/ext/bc/C/rpc"
	}
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, nil, fmt.Errorf("dial source chain: %w", err)
	}
	receipt, err := client.TransactionReceipt(ctx, common.HexToHash(txHash))
	if err != nil {
		return nil, nil, fmt.Errorf("receipt: %w", err)
	}
	if receipt.Status != 1 {
		return nil, nil, fmt.Errorf("send tx reverted")
	}

	var unsigned *avalancheWarp.UnsignedMessage
	var msg *relayer.TeleporterMessageV2
	var warpLogs, sendLogs int
	for _, l := range receipt.Logs {
		switch {
		case l.Address == relayer.WarpPrecompileAddr && len(l.Topics) > 0 && l.Topics[0] == relayer.SendWarpMessageTopic:
			warpLogs++
			var out struct{ B []byte }
			if err := relayer.BytesDecoderABI.UnpackIntoInterface(&out, "d", l.Data); err != nil {
				return nil, nil, fmt.Errorf("unpack SendWarpMessage data: %w", err)
			}
			unsigned, err = avalancheWarp.ParseUnsignedMessage(out.B)
			if err != nil {
				return nil, nil, fmt.Errorf("parse unsigned warp message: %w", err)
			}
		case l.Address == teleporter && len(l.Topics) > 0 && l.Topics[0] == relayer.SendCrossChainMessageTopic:
			sendLogs++
			var out struct {
				M relayer.TeleporterMessageV2
				F relayer.TeleporterFeeInfo
			}
			if err := relayer.EventDecoderABI.UnpackIntoInterface(&out, "d", l.Data); err != nil {
				return nil, nil, fmt.Errorf("unpack SendCrossChainMessage data: %w", err)
			}
			m := out.M
			msg = &m
		}
	}
	// A tx with several sends would silently relay only the last pair. A
	// stray warp message from another contract in the call chain would pair
	// the wrong attestation with the delivered struct. Refuse rather than
	// guess.
	if warpLogs > 1 || sendLogs > 1 {
		return nil, nil, fmt.Errorf("tx contains %d SendWarpMessage / %d SendCrossChainMessage logs — relay a tx with exactly one send", warpLogs, sendLogs)
	}
	if unsigned == nil {
		return nil, nil, fmt.Errorf("no SendWarpMessage log from the warp precompile in tx")
	}
	if msg == nil {
		return nil, nil, fmt.Errorf("no SendCrossChainMessage log from teleporter %s in tx", teleporter)
	}
	return unsigned, msg, nil
}

// --- Registered validator set (the stored snapshot of the registry) ---

// registeredValidator is one entry of the stored set of the registry: a
// 96-byte uncompressed BLS public key and its stake weight.
type registeredValidator struct {
	BlsPublicKey []byte
	Weight       uint64
}

type registeredSet struct {
	AvalancheBlockchainID [32]byte
	Validators            []registeredValidator
	TotalWeight           uint64
	PChainHeight          uint64
	PChainTimestamp       uint64
}

// validatorSetDecoderABI decodes the ValidatorSet return value of
// SubsetUpdater.getValidatorSet.
var validatorSetDecoderABI = func() abi.ABI {
	const j = `[{"type":"function","name":"d","inputs":[],"outputs":[{"name":"s","type":"tuple","components":[` +
		`{"name":"avalancheBlockchainID","type":"bytes32"},` +
		`{"name":"validators","type":"tuple[]","components":[{"name":"blsPublicKey","type":"bytes"},{"name":"weight","type":"uint64"}]},` +
		`{"name":"totalWeight","type":"uint64"},` +
		`{"name":"pChainHeight","type":"uint64"},` +
		`{"name":"pChainTimestamp","type":"uint64"}]}]}]`
	parsed, err := abi.JSON(strings.NewReader(j))
	if err != nil {
		log.Fatalf("build validator set decoder ABI: %v", err)
	}
	return parsed
}()

// fetchRegisteredSet reads the stored validator set of the registry for the
// given source chain. Signature bitset indexes MUST be computed against this
// array. The contract applies the bitset to its own storage. Thus indexes
// derived from the current P-Chain set diverge as soon as the primary set
// churns: the registration-time snapshot differs from the current set.
func fetchRegisteredSet(
	ctx context.Context,
	besuRPC string,
	registry common.Address,
	chainID [32]byte,
) (*registeredSet, error) {
	client, err := ethclient.Dial(besuRPC)
	if err != nil {
		return nil, fmt.Errorf("dial external chain: %w", err)
	}
	selector := crypto.Keccak256([]byte("getValidatorSet(bytes32)"))[:4]
	data := append(selector, chainID[:]...)
	out, err := client.CallContract(ctx, ethereum.CallMsg{To: &registry, Data: data}, nil)
	if err != nil {
		return nil, fmt.Errorf("getValidatorSet: %w", err)
	}
	var res struct{ S registeredSet }
	if err := validatorSetDecoderABI.UnpackIntoInterface(&res, "d", out); err != nil {
		return nil, fmt.Errorf("decode ValidatorSet: %w", err)
	}
	if len(res.S.Validators) == 0 {
		return nil, fmt.Errorf("registry %s has no registered set for chain %x", registry, chainID)
	}
	return &res.S, nil
}

// --- Delivery to the external chain ---

func deliver(
	ctx context.Context,
	besuRPC, teleporterArtifact, besuKeyHex string,
	teleporter common.Address,
	msg *relayer.TeleporterMessageV2,
	networkID uint32,
	sourceChainID ids.ID,
	attestation []byte,
) (*types.Receipt, error) {
	teleporterABI := relayer.MustLoadABI(teleporterArtifact)
	icm := relayer.TeleporterICMMessage{
		Message:            *msg,
		SourceNetworkID:    networkID,
		SourceBlockchainID: [32]byte(sourceChainID),
		Attestation:        attestation,
	}
	callData, err := teleporterABI.Pack("receiveCrossChainMessage", icm, common.Address{})
	if err != nil {
		return nil, fmt.Errorf("pack receiveCrossChainMessage: %w", err)
	}

	key, err := crypto.HexToECDSA(strings.TrimPrefix(besuKeyHex, "0x"))
	if err != nil {
		return nil, fmt.Errorf("parse besu key: %w", err)
	}
	client, err := ethclient.Dial(besuRPC)
	if err != nil {
		return nil, fmt.Errorf("dial external chain: %w", err)
	}
	chainID, err := client.ChainID(ctx)
	if err != nil {
		return nil, err
	}
	nonce, err := client.PendingNonceAt(ctx, ethAddress(key))
	if err != nil {
		return nil, err
	}
	tx := types.MustSignNewTx(key, types.LatestSignerForChainID(chainID), &types.DynamicFeeTx{
		ChainID: chainID, Nonce: nonce,
		GasTipCap: big.NewInt(1_000_000_000), GasFeeCap: big.NewInt(8_000_000_000),
		Gas: 7_000_000, To: &teleporter, Data: callData,
	})
	if err := client.SendTransaction(ctx, tx); err != nil {
		return nil, fmt.Errorf("send: %w", err)
	}
	receipt, err := relayer.WaitReceipt(ctx, client, tx.Hash())
	if err != nil {
		return nil, fmt.Errorf("timed out waiting for receipt %s", tx.Hash())
	}
	return receipt, nil
}

func ethAddress(key *ecdsa.PrivateKey) common.Address {
	return crypto.PubkeyToAddress(key.PublicKey)
}

// sourceRPCOverride, when set via --source-rpc, makes the relayer read the
// send tx from a chain other than the C-Chain, for example the RPC of an L1.
var sourceRPCOverride string
