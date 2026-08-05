package main

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"strings"

	"github.com/ava-labs/libevm/accounts/abi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethclient"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/sidecar/internal/relayer"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// --- Delivery via stock receiveCrossChainMessage ---

func deliver(
	ctx context.Context,
	uri, teleporterArtifactPath, ethKeyStr string,
	teleporterAddr common.Address,
	teleporterBytes []byte,
	networkID uint32,
	gatewayChainID ids.ID,
	signedMsg *avalancheWarp.Message,
) error {
	teleporterABI := relayer.MustLoadABI(teleporterArtifactPath)

	// Decode the Teleporter message from the outbox log data to rebuild the
	// receiveCrossChainMessage calldata. abi.Arguments has no UnpackIntoInterface,
	// so use a tiny decoder ABI whose single output is the TeleporterMessageV2 tuple.
	var wrap struct{ M relayer.TeleporterMessageV2 }
	if err := relayer.MessageDecoderABI.UnpackIntoInterface(&wrap, "d", teleporterBytes); err != nil {
		return fmt.Errorf("decode TeleporterMessageV2: %w", err)
	}
	msg := wrap.M

	uint32T, _ := abi.NewType("uint32", "", nil)
	attestation, err := abi.Arguments{{Type: uint32T}}.Pack(uint32(0))
	if err != nil {
		return err
	}
	icm := relayer.TeleporterICMMessage{
		Message:            msg,
		SourceNetworkID:    networkID,
		SourceBlockchainID: [32]byte(gatewayChainID),
		Attestation:        attestation,
	}
	callData, err := teleporterABI.Pack("receiveCrossChainMessage", icm, common.Address{})
	if err != nil {
		return fmt.Errorf("pack receiveCrossChainMessage: %w", err)
	}

	var fundedKey secp256k1.PrivateKey
	if err := fundedKey.UnmarshalText([]byte(ethKeyStr)); err != nil {
		return fmt.Errorf("parse eth key: %w", err)
	}
	ethKey := fundedKey.ToECDSA()
	rpcURL := destRPCOverride
	if rpcURL == "" {
		rpcURL = strings.TrimSuffix(uri, "/") + "/ext/bc/C/rpc"
	}
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return err
	}
	chainID, err := client.ChainID(ctx)
	if err != nil {
		return err
	}
	sender := crypto.PubkeyToAddress(ethKey.PublicKey)
	nonce, err := client.PendingNonceAt(ctx, sender)
	if err != nil {
		return err
	}

	tx := types.MustSignNewTx(ethKey, types.LatestSignerForChainID(chainID), &types.DynamicFeeTx{
		ChainID: chainID, Nonce: nonce, GasTipCap: big.NewInt(1_000_000_000), GasFeeCap: big.NewInt(40_000_000_000),
		Gas: 6_000_000, To: &teleporterAddr, Data: callData,
		AccessList: relayer.BuildPredicate(signedMsg.Bytes()),
	})
	if err := client.SendTransaction(ctx, tx); err != nil {
		return fmt.Errorf("send receive tx: %w", err)
	}
	receipt, err := relayer.WaitReceipt(ctx, client, tx.Hash())
	if err != nil {
		return err
	}
	if receipt.Status != 1 {
		return fmt.Errorf("receiveCrossChainMessage reverted (tx %s)", tx.Hash())
	}
	log.Printf("delivered in C-Chain block %d (tx %s)", receipt.BlockNumber, tx.Hash())
	return nil
}

// destRPCOverride, when set via --dest-rpc, redirects delivery to a chain
// other than the C-Chain (e.g. an L1's RPC).
var destRPCOverride string
