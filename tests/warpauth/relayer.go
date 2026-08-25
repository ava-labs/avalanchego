// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warpauth

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ava-labs/libevm"
	"github.com/ava-labs/libevm/common"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/graft/coreth/ethclient"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	warpclient "github.com/ava-labs/avalanchego/graft/coreth/warp"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm"
)

const (
	relayQuorum  = 67
	relayPoll    = time.Second
	relayMaxSpan = 1000 // blocks per log query
)

// Relayer carries helper-contract warp messages to the P-chain. It holds no
// keys and no funds: the fee is paid by the tx's own inputs, and racing
// relayers are harmless because the second copy of a tx is simply rejected.
type Relayer struct {
	Log    logging.Logger
	Eth    *ethclient.Client
	Warp   warpclient.Client
	PChain *platformvm.Client
	Helper common.Address
}

// Run relays every SendWarpMessage log emitted by Helper from [fromBlock]
// on, until ctx is done. Failures are logged and the block is retried, so a
// message is never skipped.
func (r *Relayer) Run(ctx context.Context, fromBlock uint64) error {
	sendWarpMessageID := warp.WarpABI.Events["SendWarpMessage"].ID
	for {
		latest, err := r.Eth.BlockNumber(ctx)
		if err != nil {
			r.Log.Warn("failed to get latest block", zap.Error(err))
		}
		for err == nil && fromBlock <= latest {
			toBlock := min(fromBlock+relayMaxSpan-1, latest)
			logs, err := r.Eth.FilterLogs(ctx, ethereum.FilterQuery{
				FromBlock: new(big.Int).SetUint64(fromBlock),
				ToBlock:   new(big.Int).SetUint64(toBlock),
				Addresses: []common.Address{warp.ContractAddress},
				Topics:    [][]common.Hash{{sendWarpMessageID}, {common.BytesToHash(r.Helper.Bytes())}},
			})
			if err != nil {
				r.Log.Warn("failed to filter logs", zap.Error(err))
				break
			}
			for _, log := range logs {
				if err := r.relay(ctx, log.Data); err != nil {
					r.Log.Warn("failed to relay message; retrying block",
						zap.Uint64("block", log.BlockNumber),
						zap.Error(err),
					)
					toBlock = log.BlockNumber - 1
					break
				}
			}
			fromBlock = toBlock + 1
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(relayPoll):
		}
	}
}

func (r *Relayer) relay(ctx context.Context, logData []byte) error {
	unsigned, err := warp.UnpackSendWarpEventDataToMessage(logData)
	if err != nil {
		return fmt.Errorf("unpacking log: %w", err)
	}
	signed, err := r.Warp.GetMessageAggregateSignature(ctx, unsigned.ID(), relayQuorum, "")
	if err != nil {
		return fmt.Errorf("aggregating signatures for %s: %w", unsigned.ID(), err)
	}
	tx, err := Wrap(signed)
	if err != nil {
		return fmt.Errorf("wrapping %s: %w", unsigned.ID(), err)
	}
	txID, err := r.PChain.IssueTx(ctx, tx.Bytes())
	if err != nil {
		return fmt.Errorf("issuing %s: %w", tx.ID(), err)
	}
	r.Log.Info("relayed warp message to the P-chain",
		zap.Stringer("messageID", unsigned.ID()),
		zap.Stringer("txID", txID),
	)
	return nil
}
