// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warpauth

import (
	"context"
	"fmt"
	"math/big"
	"time"

	ethereum "github.com/ava-labs/libevm"
	"github.com/ava-labs/libevm/common"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/graft/coreth/ethclient"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	avalanchewarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

const (
	relayPoll    = time.Second
	relayMaxSpan = 1000 // blocks per log query
	maxUTXOs     = 1024 // platform.getUTXOs page cap
	// ponytail: bounded so a tx the P-chain will never take (bad fee, bad
	// auth) cannot stall the relayer; raise if slow blocks cause drops.
	relayMaxAttempts = 10
)

// Relayer carries helper-contract warp messages to the P-chain. It holds no
// keys and no funds: the fee is paid by the tx's own inputs. A message whose
// inputs are already spent (by this relayer before a restart, by another
// relayer, or by the owner) is dropped, so rescanning old blocks is harmless
// and racing relayers cannot stall each other.
type Relayer struct {
	Log logging.Logger
	Eth *ethclient.Client
	// Sign returns the message with an aggregated BLS signature of at least
	// relayQuorum percent of the primary network stake.
	Sign   func(context.Context, *avalanchewarp.UnsignedMessage) ([]byte, error)
	PChain *platformvm.Client
	Helper common.Address
}

// Run relays every SendWarpMessage log emitted by Helper from [fromBlock]
// on, until ctx is done. RPC failures are logged and the block is retried;
// a message the P-chain cannot accept is logged and dropped.
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
		r.Log.Warn("dropping unparsable log", zap.Error(err))
		return nil
	}
	signed, err := r.Sign(ctx, unsigned)
	if err != nil {
		return fmt.Errorf("aggregating signatures for %s: %w", unsigned.ID(), err)
	}
	tx, owner, err := Wrap(signed)
	if err != nil {
		r.Log.Warn("dropping message with unparsable tx", zap.Stringer("messageID", unsigned.ID()), zap.Error(err))
		return nil
	}

	// Issue until it lands or an input disappears: a missing input means the
	// tx was accepted (ours or another relayer's) or was wrong to begin with.
	for attempt := 1; ; attempt++ {
		spendable, err := r.spendableUTXOs(ctx, owner, tx.Unsigned)
		if err != nil {
			return fmt.Errorf("fetching UTXOs of %s: %w", owner, err)
		}
		missing := tx.Unsigned.InputIDs()
		missing.Difference(spendable)
		if missing.Len() > 0 {
			r.Log.Info("dropping message whose inputs are spent",
				zap.Stringer("messageID", unsigned.ID()),
				zap.Stringer("txID", tx.ID()),
			)
			return nil
		}
		txID, err := r.PChain.IssueTx(ctx, tx.Bytes())
		if err == nil {
			r.Log.Info("relayed warp message to the P-chain",
				zap.Stringer("messageID", unsigned.ID()),
				zap.Stringer("txID", txID),
			)
			return nil
		}
		if attempt == relayMaxAttempts {
			r.Log.Warn("P-chain kept rejecting tx; dropping message",
				zap.Stringer("messageID", unsigned.ID()),
				zap.Stringer("txID", tx.ID()),
				zap.Error(err),
			)
			return nil
		}
		r.Log.Info("P-chain rejected tx; rechecking inputs",
			zap.Stringer("txID", tx.ID()),
			zap.Error(err),
		)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(relayPoll):
		}
	}
}

// spendableUTXOs returns the IDs of the UTXOs [owner] can spend in [unsigned]:
// its P-chain UTXOs, plus the shared-memory UTXOs an ImportTx pulls in.
func (r *Relayer) spendableUTXOs(ctx context.Context, owner ids.ShortID, unsigned txs.UnsignedTx) (set.Set[ids.ID], error) {
	sourceChains := []string{""}
	if importTx, ok := unsigned.(*txs.ImportTx); ok {
		sourceChains = append(sourceChains, importTx.SourceChain.String())
	}
	utxoIDs := set.Set[ids.ID]{}
	for _, sourceChain := range sourceChains {
		utxosBytes, _, _, err := r.PChain.GetAtomicUTXOs(ctx, []ids.ShortID{owner}, sourceChain, maxUTXOs, ids.ShortEmpty, ids.Empty)
		if err != nil {
			return nil, err
		}
		for _, utxoBytes := range utxosBytes {
			utxo := &avax.UTXO{}
			if _, err := txs.Codec.Unmarshal(utxoBytes, utxo); err != nil {
				return nil, err
			}
			utxoIDs.Add(utxo.InputID())
		}
	}
	return utxoIDs, nil
}
