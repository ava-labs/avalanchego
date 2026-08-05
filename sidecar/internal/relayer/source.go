package relayer

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/ava-labs/libevm/accounts/abi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethclient"

	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// FetchTeleporterSend reads a Teleporter send transaction on any Avalanche
// EVM chain and returns the unsigned warp message (from the precompile's
// SendWarpMessage log) and the TeleporterMessageV2 struct (from the
// Teleporter's SendCrossChainMessage log). Shared by every relayer whose
// source is an Avalanche chain.
func FetchTeleporterSend(
	ctx context.Context,
	rpcURL, txHash string,
	teleporter common.Address,
) (*avalancheWarp.UnsignedMessage, *TeleporterMessageV2, error) {
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
	var msg *TeleporterMessageV2
	var warpLogs, sendLogs int
	for _, l := range receipt.Logs {
		switch {
		case l.Address == WarpPrecompileAddr && len(l.Topics) > 0 && l.Topics[0] == SendWarpMessageTopic:
			warpLogs++
			var out struct{ B []byte }
			if err := BytesDecoderABI.UnpackIntoInterface(&out, "d", l.Data); err != nil {
				return nil, nil, fmt.Errorf("unpack SendWarpMessage data: %w", err)
			}
			unsigned, err = avalancheWarp.ParseUnsignedMessage(out.B)
			if err != nil {
				return nil, nil, fmt.Errorf("parse unsigned warp message: %w", err)
			}
		case l.Address == teleporter && len(l.Topics) > 0 && l.Topics[0] == SendCrossChainMessageTopic:
			sendLogs++
			var out struct {
				M TeleporterMessageV2
				F TeleporterFeeInfo
			}
			if err := EventDecoderABI.UnpackIntoInterface(&out, "d", l.Data); err != nil {
				return nil, nil, fmt.Errorf("unpack SendCrossChainMessage data: %w", err)
			}
			m := out.M
			msg = &m
		}
	}
	// A tx with several sends would silently relay only the last pair. Refuse
	// rather than guess.
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

// MustLoadABI reads a foundry artifact and returns its ABI, exiting on any
// error — relayers treat a bad artifact path as fatal configuration.
func MustLoadABI(path string) abi.ABI {
	raw, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("read ABI %s: %v", path, err)
	}
	var a struct {
		ABI json.RawMessage `json:"abi"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		log.Fatalf("parse artifact %s: %v", path, err)
	}
	parsed, err := abi.JSON(strings.NewReader(string(a.ABI)))
	if err != nil {
		log.Fatalf("parse ABI %s: %v", path, err)
	}
	return parsed
}
