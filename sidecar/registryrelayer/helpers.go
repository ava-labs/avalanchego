package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strings"

	"github.com/ava-labs/libevm/accounts/abi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethclient"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/sidecar/internal/relayer"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// fetchCChainSend reads the C-Chain tx and returns the unsigned warp message
// (from the precompile's SendWarpMessage log) and the TeleporterMessageV2
// struct (from the Teleporter's SendCrossChainMessage log).
func fetchCChainSend(
	ctx context.Context,
	avalancheURI, txHash string,
	teleporter common.Address,
) (*avalancheWarp.UnsignedMessage, *relayer.TeleporterMessageV2, error) {
	client, err := ethclient.Dial(strings.TrimSuffix(avalancheURI, "/") + "/ext/bc/C/rpc")
	if err != nil {
		return nil, nil, fmt.Errorf("dial C-Chain: %w", err)
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
	// A tx with several sends would silently relay only the last pair — and a
	// stray warp message from another contract in the call chain would pair the
	// wrong attestation with the delivered struct. Refuse rather than guess.
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
	teleporterABI, err := loadABI(teleporterArtifact)
	if err != nil {
		return nil, err
	}
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
		GasTipCap: big.NewInt(1_000_000_000), GasFeeCap: big.NewInt(100_000_000_000),
		Gas: 8_000_000, To: &teleporter, Data: callData,
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

func loadABI(path string) (abi.ABI, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return abi.ABI{}, fmt.Errorf("read %s: %w", path, err)
	}
	var a struct {
		ABI json.RawMessage `json:"abi"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return abi.ABI{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return abi.JSON(strings.NewReader(string(a.ABI)))
}
