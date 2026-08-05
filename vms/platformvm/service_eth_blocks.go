// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	txexecutor "github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"

	ethcommon "github.com/ava-labs/libevm/common"
	ethtypes "github.com/ava-labs/libevm/core/types"
)

// ethBlockGasLimit is the static gas limit reported in synthesized blocks.
// P-chain capacity is enforced by the fee mechanism, not a block gas limit;
// this exists so wallet math (gasUsed <= gasLimit) always holds.
const ethBlockGasLimit = 100_000_000

// resolveBlockTag maps an eth block tag or hex number to a P-chain height.
// The P-chain has instant finality, so latest, pending, safe and finalized are
// all the last accepted height. Returns ok=false for heights past the tip.
func (a *ethAPI) resolveBlockTag(tag string) (uint64, bool, error) {
	last, err := a.lastAcceptedHeight()
	if err != nil {
		return 0, false, err
	}
	switch tag {
	case "latest", "pending", "safe", "finalized":
		return last, true, nil
	case "earliest":
		return 0, true, nil
	}
	if !strings.HasPrefix(tag, "0x") {
		return 0, false, fmt.Errorf("bad block tag %q", tag)
	}
	var height uint64
	if _, err := fmt.Sscanf(tag, "0x%x", &height); err != nil {
		return 0, false, fmt.Errorf("bad block number %q: %w", tag, err)
	}
	return height, height <= last, nil
}

func (a *ethAPI) blockByHeight(height uint64, fullTxs bool) (any, error) {
	blkID, err := a.vm.state.GetBlockIDAtHeight(height)
	if err == database.ErrNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	blk, err := a.vm.state.GetStatelessBlock(blkID)
	if err != nil {
		return nil, err
	}
	return a.ethBlock(blk, fullTxs)
}

// ethBlock synthesizes an eth block from a P-chain block: hash is the block ID
// bytes, number is the height, and the tx list contains the eth txs it holds.
func (a *ethAPI) ethBlock(blk block.Block, fullTxs bool) (map[string]any, error) {
	var (
		txHashes []any
		allLogs  []*ethtypes.Log
		gasUsed  uint64
		txIndex  uint32
	)
	for _, tx := range blk.Txs() {
		unsigned, ok := tx.Unsigned.(*txs.EthRLPTx)
		if !ok {
			continue
		}
		if err := unsigned.SyntacticVerify(a.vm.ctx); err != nil {
			return nil, err
		}
		hash := unsigned.Parsed.Hash()
		record, found, err := a.receiptRecord(tx.ID())
		if err != nil {
			return nil, err
		}
		if !found {
			// The block is accepted, so index it and retry once.
			if err := a.scanAcceptedBlocks(); err != nil {
				return nil, err
			}
			if record, found, err = a.receiptRecord(tx.ID()); err != nil || !found {
				return nil, fmt.Errorf("accepted eth tx %s is not indexed: %w", hash, err)
			}
		}
		gasUsed += record.gasUsed
		allLogs = append(allLogs, stakeLogs(unsigned, &record, hash)...)
		if fullTxs {
			txHashes = append(txHashes, a.ethTxObject(unsigned, &record))
		} else {
			txHashes = append(txHashes, hash.Hex())
		}
		txIndex++
	}
	if txHashes == nil {
		txHashes = []any{}
	}

	parentID := blk.Parent()
	blkID := blk.ID()
	// geth's ethclient rejects a block whose transactionsRoot says "empty"
	// while the tx list is not (and vice versa), so mark non-empty blocks with
	// a synthetic root derived from the block ID.
	txRoot := ethtypes.EmptyRootHash.Hex()
	if len(txHashes) > 0 {
		txRoot = "0x" + hex.EncodeToString(blkID[:])
	}
	return map[string]any{
		"number":           hexUint(blk.Height()),
		"hash":             "0x" + hex.EncodeToString(blkID[:]),
		"parentHash":       "0x" + hex.EncodeToString(parentID[:]),
		"timestamp":        hexUint(blockTimestamp(blk)),
		"transactions":     txHashes,
		"gasLimit":         hexUint(ethBlockGasLimit),
		"gasUsed":          hexUint(gasUsed),
		"baseFeePerGas":    "0x" + a.gasPriceWei().Text(16),
		"miner":            ethcommon.Address{}.Hex(),
		"difficulty":       "0x0",
		"totalDifficulty":  "0x0",
		"nonce":            "0x0000000000000000",
		"extraData":        "0x",
		"mixHash":          ethcommon.Hash{}.Hex(),
		"sha3Uncles":       ethtypes.EmptyUncleHash.Hex(),
		"stateRoot":        ethtypes.EmptyRootHash.Hex(),
		"transactionsRoot": txRoot,
		"receiptsRoot":     ethtypes.EmptyRootHash.Hex(),
		"logsBloom":        logsBloomHex(allLogs),
		"size":             hexUint(uint64(len(blk.Bytes()))),
		"uncles":           []any{},
	}, nil
}

// ethTxObject synthesizes the eth_getTransactionByHash form of an accepted or
// pending tx. record is nil for pending txs.
func (a *ethAPI) ethTxObject(unsigned *txs.EthRLPTx, record *ethReceiptRecord) map[string]any {
	parsed := unsigned.Parsed
	v, r, s := parsed.RawSignatureValues()
	obj := map[string]any{
		"hash":                 parsed.Hash().Hex(),
		"nonce":                hexUint(parsed.Nonce()),
		"from":                 ethcommon.Address(unsigned.Sender).Hex(),
		"to":                   ethcommon.Address(unsigned.Recipient).Hex(),
		"value":                "0x" + parsed.Value().Text(16),
		"gas":                  hexUint(parsed.Gas()),
		"maxFeePerGas":         "0x" + parsed.GasFeeCap().Text(16),
		"maxPriorityFeePerGas": "0x" + parsed.GasTipCap().Text(16),
		"input":                "0x" + hex.EncodeToString(parsed.Data()),
		"chainId":              "0x" + txs.EthRLPChainID(a.vm.ctx.NetworkID).Text(16),
		"type":                 "0x2",
		"accessList":           []any{},
		"v":                    "0x" + v.Text(16),
		"r":                    "0x" + r.Text(16),
		"s":                    "0x" + s.Text(16),
		"blockHash":            nil,
		"blockNumber":          nil,
		"transactionIndex":     nil,
		"gasPrice":             "0x" + parsed.GasFeeCap().Text(16),
	}
	if record != nil {
		obj["blockHash"] = "0x" + hex.EncodeToString(record.blkID[:])
		obj["blockNumber"] = hexUint(record.height)
		obj["transactionIndex"] = hexUint(uint64(record.txIndex))
		obj["gasPrice"] = hexUint(record.priceWei)
	}
	return obj
}

func (a *ethAPI) getTransactionByHash(hash ethcommon.Hash) (any, error) {
	txID, ok, err := a.txIDOf(hash)
	if err != nil || !ok {
		return nil, err
	}

	raw, err := a.rlpOf(txID)
	if err == database.ErrNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	unsigned := &txs.EthRLPTx{RLP: raw}
	if err := unsigned.SyntacticVerify(a.vm.ctx); err != nil {
		return nil, err
	}

	record, found, err := a.receiptRecord(txID)
	if err != nil {
		return nil, err
	}
	if !found {
		if err := a.scanAcceptedBlocks(); err != nil {
			return nil, err
		}
		record, found, err = a.receiptRecord(txID)
		if err != nil {
			return nil, err
		}
	}
	if !found {
		return a.ethTxObject(unsigned, nil), nil // pending (or dropped)
	}
	return a.ethTxObject(unsigned, &record), nil
}

// feeHistory returns a minimal EIP-1559 fee history: a flat series at the
// current price with zero rewards, which steers MetaMask's default fee flow to
// maxFeePerGas = current price and zero priority fee. Both are acceptable to
// the executor.
func (a *ethAPI) feeHistory(params []json.RawMessage) (any, error) {
	var countHex string
	if err := parseParam(params, 0, &countHex); err != nil {
		return nil, err
	}
	var count uint64
	if _, err := fmt.Sscanf(countHex, "0x%x", &count); err != nil {
		// Some clients send a plain number.
		if err := json.Unmarshal(params[0], &count); err != nil {
			return nil, fmt.Errorf("bad block count %q", countHex)
		}
	}
	if count == 0 || count > 1024 {
		count = 1
	}

	var percentiles []float64
	if len(params) > 2 {
		_ = json.Unmarshal(params[2], &percentiles) // optional
	}

	last, err := a.lastAcceptedHeight()
	if err != nil {
		return nil, err
	}
	oldest := uint64(0)
	if last >= count-1 {
		oldest = last - (count - 1)
	}

	price := "0x" + a.gasPriceWei().Text(16)
	baseFees := make([]string, count+1)
	gasRatios := make([]float64, count)
	rewards := make([][]string, count)
	zeroRewards := make([]string, len(percentiles))
	for i := range zeroRewards {
		zeroRewards[i] = "0x0"
	}
	for i := range gasRatios {
		baseFees[i] = price
		rewards[i] = zeroRewards
	}
	baseFees[count] = price

	return map[string]any{
		"oldestBlock":   hexUint(oldest),
		"baseFeePerGas": baseFees,
		"gasUsedRatio":  gasRatios,
		"reward":        rewards,
	}, nil
}

// blockTimestamp is the Banff block timestamp; pre-Banff blocks (genesis on a
// fresh devnet) report zero.
func blockTimestamp(blk block.Block) uint64 {
	if banff, ok := blk.(block.BanffBlock); ok {
		return uint64(banff.Timestamp().Unix())
	}
	return 0
}

func boolParam(params []json.RawMessage, i int) bool {
	var v bool
	if i < len(params) {
		_ = json.Unmarshal(params[i], &v)
	}
	return v
}

// estimateGas runs the real selection walk against current state, so the answer
// is the gas execution will charge for that send. It is state-dependent: a UTXO
// arriving before the tx lands can change how many inputs are needed, which is
// EVM-normal (a state change between estimate and execution moves gas there
// too). The result is what the caller should sign as its gas limit.
func (a *ethAPI) estimateGas(call *ethCallArgs) (any, error) {
	if call.From == nil {
		// Without a sender there is nothing to select from; price the
		// single-input case, which is the floor for any send.
		txGas, err := fee.EthRLPTxComplexity(
			txs.MaxEthRLPEnvelopeBytes+len(call.calldata()), 1,
		).ToGas(a.vm.DynamicFeeConfig.Weights)
		if err != nil {
			return nil, err
		}
		return hexUint(uint64(txGas)), nil
	}

	amount, err := weiToNAVAX(call.Value)
	if err != nil {
		return nil, err
	}

	// The serialized length is unknown before signing, so price the envelope
	// bound plus calldata. Bandwidth weighs 1 per byte, so this overshoots the
	// eventual charge by at most the unused envelope slack.
	rlpLen := txs.MaxEthRLPEnvelopeBytes + len(call.calldata())
	spender := txexecutor.NewEthSpender(
		a.vm.state,
		a.vm.DynamicFeeConfig.Weights,
		a.vm.ctx.AVAXAssetID,
		state.PickFeeCalculator(&a.vm.Internal, a.vm.state),
	)
	spend, err := spender.SelectInputs(
		ids.ShortID(*call.From),
		amount,
		rlpLen,
		// Estimating is not spending, so the walk is bounded only by the
		// structural ceiling here.
		math.MaxUint64,
	)
	if err != nil {
		return nil, err
	}
	return hexUint(uint64(spend.Gas)), nil
}

// weiToNAVAX converts a call object's value, rejecting sub-nAVAX dust the same
// way tx verification does.
func weiToNAVAX(value *hexBig) (uint64, error) {
	if value == nil {
		return 0, nil
	}
	amount, rem := new(big.Int).QuoRem(value.toInt(), txs.WeiPerNAVAX, new(big.Int))
	switch {
	case rem.Sign() != 0:
		return 0, txs.ErrValueDust
	case !amount.IsUint64():
		return 0, fmt.Errorf("value %s overflows uint64 nAVAX", value.toInt())
	}
	return amount.Uint64(), nil
}
