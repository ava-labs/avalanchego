// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"cmp"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"slices"
	"strings"

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	safemath "github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	ethcommon "github.com/ava-labs/libevm/common"
)

// ethAPI is an Ethereum JSON-RPC facade over the P-chain so that stock EVM
// tooling can read balances and issue EthRLPTxs. Single requests only.
// ponytail: no batch requests, no logs, no historical block queries.
type ethAPI struct {
	vm *VM

	// indexDB persists eth hash -> txID, txID -> raw RLP and txID -> inclusion
	// block for accepted txs. Node-local, not consensus state.
	indexDB database.Database

	// Submitted but not yet accepted txs, so lookups work before inclusion
	// without letting unauthenticated submissions write to disk.
	pending    *lru.Cache[ethcommon.Hash, ids.ID]
	pendingRLP *lru.Cache[ids.ID, []byte]

	// stakedCache memoizes the staked-balance walk for one block.
	stakedCache stakedBalances
}

var (
	ethIndexPrefix = []byte("ethTxIndex")

	// index key namespaces
	ethHashKeyPrefix    = []byte("h") // eth tx hash -> platform txID
	ethReceiptKeyPrefix = []byte("b") // platform txID -> receipt record
	ethRLPKeyPrefix     = []byte("r") // platform txID -> raw eth tx RLP
	ethWatermarkKey     = []byte("w") // last scanned block height
)

const (
	pendingEthTxCacheSize = 2048

	// maxIndexScanBlocks bounds how many blocks one request may index.
	maxIndexScanBlocks = 1024
)

func newEthAPI(vm *VM) *ethAPI {
	a := &ethAPI{
		vm:         vm,
		indexDB:    prefixdb.New(ethIndexPrefix, vm.db),
		pending:    lru.NewCache[ethcommon.Hash, ids.ID](pendingEthTxCacheSize),
		pendingRLP: lru.NewCache[ids.ID, []byte](pendingEthTxCacheSize),
	}
	// Seed the index at the tip so no request can ever trigger a walk of the
	// whole chain. Called at chain registration, under the chain lock.
	if _, err := database.GetUInt64(a.indexDB, ethWatermarkKey); err == database.ErrNotFound {
		if height, err := a.lastAcceptedHeight(); err == nil {
			_ = database.PutUInt64(a.indexDB, ethWatermarkKey, height)
		}
	}
	return a
}

func (a *ethAPI) chainID() *big.Int {
	return txs.EthRLPChainID(a.vm.ctx.NetworkID)
}

// txIDOf resolves an eth tx hash to its platform txID, checking the accepted
// index first and then the pending cache.
func (a *ethAPI) txIDOf(hash ethcommon.Hash) (ids.ID, bool, error) {
	txIDBytes, err := a.indexDB.Get(hashKey(hash))
	if err == nil {
		txID, err := ids.ToID(txIDBytes)
		return txID, true, err
	}
	if err != database.ErrNotFound {
		return ids.Empty, false, err
	}
	txID, ok := a.pending.Get(hash)
	return txID, ok, nil
}

// rlpOf returns the raw eth tx bytes for an accepted or pending tx.
func (a *ethAPI) rlpOf(txID ids.ID) ([]byte, error) {
	raw, err := a.indexDB.Get(rlpKey(txID))
	if err == nil {
		return raw, nil
	}
	if err != database.ErrNotFound {
		return nil, err
	}
	if raw, ok := a.pendingRLP.Get(txID); ok {
		return raw, nil
	}
	return nil, database.ErrNotFound
}

type ethRequest struct {
	ID     json.RawMessage   `json:"id"`
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
}

func (a *ethAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req ethRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := a.call(&req)
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      req.ID,
	}
	if err != nil {
		resp["error"] = map[string]any{"code": -32000, "message": err.Error()}
	} else {
		resp["result"] = result
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (a *ethAPI) call(req *ethRequest) (any, error) {
	// Tx issuance must not hold the context lock: the mempool verifier takes
	// it itself (see LockedTxVerifier), same reason Service.IssueTx doesn't.
	if req.Method == "eth_sendRawTransaction" {
		var raw hexBytes
		if err := parseParam(req.Params, 0, &raw); err != nil {
			return nil, err
		}
		return a.sendRawTransaction(raw)
	}

	a.vm.ctx.Lock.Lock()
	defer a.vm.ctx.Lock.Unlock()

	switch req.Method {
	case "eth_chainId":
		return "0x" + a.chainID().Text(16), nil

	case "net_version":
		return a.chainID().String(), nil

	case "eth_blockNumber":
		height, err := a.lastAcceptedHeight()
		return hexUint(height), err

	case "eth_gasPrice":
		return "0x" + a.gasPriceWei().Text(16), nil

	case "eth_maxPriorityFeePerGas":
		// The fee charged is exactly gas * price; tips buy nothing.
		return "0x0", nil

	case "eth_feeHistory":
		return a.feeHistory(req.Params)

	case "eth_getBlockByNumber":
		var tag string
		if err := parseParam(req.Params, 0, &tag); err != nil {
			return nil, err
		}
		height, ok, err := a.resolveBlockTag(tag)
		if err != nil || !ok {
			return nil, err
		}
		return a.blockByHeight(height, boolParam(req.Params, 1))

	case "eth_getBlockByHash":
		var hash ethcommon.Hash
		if err := parseParam(req.Params, 0, &hash); err != nil {
			return nil, err
		}
		blk, err := a.vm.state.GetStatelessBlock(ids.ID(hash))
		if err == database.ErrNotFound {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return a.ethBlock(blk, boolParam(req.Params, 1))

	case "eth_getTransactionByHash":
		var hash ethcommon.Hash
		if err := parseParam(req.Params, 0, &hash); err != nil {
			return nil, err
		}
		return a.getTransactionByHash(hash)

	case "eth_call":
		var call ethCallArgs
		if err := parseParam(req.Params, 0, &call); err != nil {
			return nil, err
		}
		return a.ethCall(call.To, call.calldata())

	case "eth_getLogs":
		var filter logFilter
		if err := parseParam(req.Params, 0, &filter); err != nil {
			return nil, err
		}
		return a.getLogs(&filter)

	case "eth_estimateGas":
		var call ethCallArgs
		if err := parseParam(req.Params, 0, &call); err != nil {
			return nil, err
		}
		return a.estimateGas(&call)

	case "eth_getBalance":
		var addr ethcommon.Address
		if err := parseParam(req.Params, 0, &addr); err != nil {
			return nil, err
		}
		balance, err := a.liquidBalance(ids.ShortID(addr))
		if err != nil {
			return nil, err
		}
		wei := new(big.Int).Mul(new(big.Int).SetUint64(balance), txs.WeiPerNAVAX)
		return "0x" + wei.Text(16), nil

	case "eth_getTransactionCount":
		var addr ethcommon.Address
		if err := parseParam(req.Params, 0, &addr); err != nil {
			return nil, err
		}
		nonce, err := a.vm.state.GetNextNonce(ids.ShortID(addr))
		return hexUint(nonce), err

	case "eth_getCode":
		var addr ethcommon.Address
		if err := parseParam(req.Params, 0, &addr); err != nil {
			return nil, err
		}
		// Ordinary accounts hold no code. The two system addresses report a
		// single INVALID byte: enough for a wallet to treat them as contracts
		// rather than as accounts that could hold or return funds, without
		// pretending to expose runtime bytecode that no EVM will execute.
		switch addr {
		case txs.EthStakingAddress, txs.EthStakedAVAXAddress:
			return "0xfe", nil
		default:
			return "0x", nil
		}

	case "eth_getTransactionReceipt":
		var hash ethcommon.Hash
		if err := parseParam(req.Params, 0, &hash); err != nil {
			return nil, err
		}
		return a.getTransactionReceipt(hash)

	default:
		return nil, fmt.Errorf("method %s not supported", req.Method)
	}
}

func (a *ethAPI) sendRawTransaction(raw []byte) (any, error) {
	unsigned := &txs.EthRLPTx{RLP: raw}
	if err := unsigned.SyntacticVerify(a.vm.ctx); err != nil {
		return nil, err
	}
	tx, err := txs.NewSigned(unsigned, txs.Codec, nil)
	if err != nil {
		return nil, err
	}

	if err := a.vm.issueTxFromRPC(tx); err != nil {
		return nil, err
	}

	// Only accepted txs are indexed on disk, so a rejected or spammed
	// submission leaves nothing behind. Pending lookups are served from a
	// bounded in-memory cache until the tx is accepted and indexed.
	hash := unsigned.Parsed.Hash()
	a.pending.Put(hash, tx.ID())
	a.pendingRLP.Put(tx.ID(), raw)
	return hash.Hex(), nil
}

// ethReceiptRecord pins an accepted eth tx to its inclusion block. txIndex is
// the tx's position among the eth txs of that block.
type ethReceiptRecord struct {
	height   uint64
	blkID    ids.ID
	gasUsed  uint64
	priceWei uint64 // effective gas price in wei per gas
	txIndex  uint32
}

const ethReceiptRecordLen = 8 + ids.IDLen + 8 + 8 + 4

func (r *ethReceiptRecord) marshal() []byte {
	data := make([]byte, ethReceiptRecordLen)
	binary.BigEndian.PutUint64(data, r.height)
	copy(data[8:], r.blkID[:])
	binary.BigEndian.PutUint64(data[8+ids.IDLen:], r.gasUsed)
	binary.BigEndian.PutUint64(data[8+ids.IDLen+8:], r.priceWei)
	binary.BigEndian.PutUint32(data[8+ids.IDLen+16:], r.txIndex)
	return data
}

func (r *ethReceiptRecord) unmarshal(data []byte) error {
	if len(data) != ethReceiptRecordLen {
		return fmt.Errorf("bad eth receipt record length %d", len(data))
	}
	r.height = binary.BigEndian.Uint64(data)
	copy(r.blkID[:], data[8:])
	r.gasUsed = binary.BigEndian.Uint64(data[8+ids.IDLen:])
	r.priceWei = binary.BigEndian.Uint64(data[8+ids.IDLen+8:])
	r.txIndex = binary.BigEndian.Uint32(data[8+ids.IDLen+16:])
	return nil
}

func (a *ethAPI) getTransactionReceipt(hash ethcommon.Hash) (any, error) {
	txID, ok, err := a.txIDOf(hash)
	if err != nil || !ok {
		return nil, err // unknown to this node
	}

	record, found, err := a.receiptRecord(txID)
	if err != nil {
		return nil, err
	}
	if !found {
		// Not indexed yet: scan newly accepted blocks, then retry once.
		if err := a.scanAcceptedBlocks(); err != nil {
			return nil, err
		}
		if record, found, err = a.receiptRecord(txID); err != nil || !found {
			return nil, err // still pending (or dropped): no receipt
		}
	}

	tx, _, err := a.vm.state.GetTx(txID)
	if err != nil {
		return nil, err
	}
	unsigned, ok := tx.Unsigned.(*txs.EthRLPTx)
	if !ok {
		return nil, nil
	}
	if err := unsigned.SyntacticVerify(a.vm.ctx); err != nil {
		return nil, err
	}

	sender := ethcommon.Address(unsigned.Sender)
	recipient := ethcommon.Address(unsigned.Recipient)
	logs := stakeLogs(unsigned, &record, hash)
	return map[string]any{
		"transactionHash":   hash.Hex(),
		"transactionIndex":  hexUint(uint64(record.txIndex)),
		"blockHash":         "0x" + hex.EncodeToString(record.blkID[:]),
		"blockNumber":       hexUint(record.height),
		"from":              sender.Hex(),
		"to":                recipient.Hex(),
		"status":            "0x1",
		"type":              "0x2",
		"gasUsed":           hexUint(record.gasUsed),
		"cumulativeGasUsed": hexUint(record.gasUsed),
		"effectiveGasPrice": hexUint(record.priceWei),
		"contractAddress":   nil,
		"logs":              logs,
		"logsBloom":         logsBloomHex(logs),
	}, nil
}

func (a *ethAPI) receiptRecord(txID ids.ID) (ethReceiptRecord, bool, error) {
	var record ethReceiptRecord
	data, err := a.indexDB.Get(receiptKey(txID))
	if err == database.ErrNotFound {
		return record, false, nil
	}
	if err != nil {
		return record, false, err
	}
	return record, true, record.unmarshal(data)
}

// scanAcceptedBlocks indexes every EthRLPTx in blocks accepted since the last
// scan. Called with the context lock held.
func (a *ethAPI) scanAcceptedBlocks() error {
	last, err := a.lastAcceptedHeight()
	if err != nil {
		return err
	}
	watermark, err := database.GetUInt64(a.indexDB, ethWatermarkKey)
	switch {
	case err == database.ErrNotFound:
		// newEthAPI seeds this at the tip; only reachable if that failed.
		return database.PutUInt64(a.indexDB, ethWatermarkKey, last)
	case err != nil:
		return err
	}

	// Bound the work per request. If this node fell behind, later calls catch
	// up rather than one call scanning arbitrarily far.
	if last-watermark > maxIndexScanBlocks {
		last = watermark + maxIndexScanBlocks
	}

	// The exact gas price at inclusion is not retrievable from state history,
	// so the record carries the price at index time. This matches whenever
	// Excess has not moved between inclusion and indexing.
	// ponytail: exact historical price needs per-height fee state persistence.
	priceWei := a.gasPriceWei()

	for h := watermark + 1; h <= last; h++ {
		blkID, err := a.vm.state.GetBlockIDAtHeight(h)
		if err != nil {
			return err
		}
		blk, err := a.vm.state.GetStatelessBlock(blkID)
		if err != nil {
			return err
		}
		txIndex := uint32(0)
		for _, tx := range blk.Txs() {
			unsigned, ok := tx.Unsigned.(*txs.EthRLPTx)
			if !ok {
				continue
			}
			if err := unsigned.SyntacticVerify(a.vm.ctx); err != nil {
				return err
			}
			txID := tx.ID()
			txGas, ok := a.vm.ethGasUsed.Get(txID)
			if !ok {
				// This node did not execute the tx itself (it was bootstrapped
				// or restarted), so the consumed input count is not recoverable
				// and the reservation is the honest upper bound.
				reserved, err := fee.EthRLPTxMaxComplexity(len(unsigned.RLP)).
					ToGas(a.vm.DynamicFeeConfig.Weights)
				if err != nil {
					return err
				}
				txGas = uint64(reserved)
			}
			record := ethReceiptRecord{
				height:   h,
				blkID:    blkID,
				gasUsed:  txGas,
				priceWei: priceWei.Uint64(),
				txIndex:  txIndex,
			}
			if err := errors.Join(
				a.indexDB.Put(hashKey(unsigned.Parsed.Hash()), txID[:]),
				a.indexDB.Put(receiptKey(txID), record.marshal()),
				a.indexDB.Put(rlpKey(txID), unsigned.RLP),
			); err != nil {
				return err
			}
			txIndex++
		}
		if err := database.PutUInt64(a.indexDB, ethWatermarkKey, h); err != nil {
			return err
		}
	}
	return nil
}

// liquidBalance is the amount [addr] can spend in one tx at the structural
// ceiling: the sum of the MaxEthRLPTxInputs largest spendable AVAX UTXOs it
// owns. Reporting the full total instead would make a wallet's Max button
// propose a tx that cannot execute.
//
// A specific tx may reach less than this, because its signed gas limit also
// bounds how many of those UTXOs selection may consume. The ceiling is the
// stable answer: it does not depend on a gas limit the caller has not chosen
// yet, and eth_estimateGas reports the gas a given send actually needs.
func (a *ethAPI) liquidBalance(addr ids.ShortID) (uint64, error) {
	utxoIDs, err := a.vm.state.UTXOIDs(addr.Bytes(), ids.Empty, math.MaxInt)
	if err != nil {
		return 0, err
	}
	chainTime := uint64(a.vm.state.GetTimestamp().Unix())
	seen := set.NewSet[ids.ID](len(utxoIDs))
	amounts := make([]uint64, 0, len(utxoIDs))
	for _, utxoID := range utxoIDs {
		if seen.Contains(utxoID) {
			continue
		}
		seen.Add(utxoID)
		utxo, err := a.vm.state.GetUTXO(utxoID)
		if err != nil {
			return 0, err
		}
		if utxo.AssetID() != a.vm.ctx.AVAXAssetID {
			continue
		}
		out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			continue
		}
		if out.Locktime > chainTime ||
			out.Threshold != 1 ||
			len(out.Addrs) != 1 ||
			out.Addrs[0] != addr {
			continue
		}
		amounts = append(amounts, out.Amt)
	}
	slices.SortFunc(amounts, func(a, b uint64) int {
		return cmp.Compare(b, a) // descending
	})

	var balance uint64
	for i, amt := range amounts {
		if i == txs.MaxEthRLPTxInputs {
			break
		}
		balance, err = safemath.Add(balance, amt)
		if err != nil {
			return 0, err
		}
	}
	return balance, nil
}

// gasPriceWei is the current P-chain gas price expressed in wei per gas.
func (a *ethAPI) gasPriceWei() *big.Int {
	price := gas.CalculatePrice(
		a.vm.DynamicFeeConfig.MinPrice,
		a.vm.state.GetFeeState().Excess,
		a.vm.DynamicFeeConfig.ExcessConversionConstant,
	)
	return new(big.Int).Mul(new(big.Int).SetUint64(uint64(price)), txs.WeiPerNAVAX)
}

func (a *ethAPI) lastAcceptedHeight() (uint64, error) {
	blk, err := a.vm.state.GetStatelessBlock(a.vm.state.GetLastAccepted())
	if err != nil {
		return 0, err
	}
	return blk.Height(), nil
}

func hashKey(hash ethcommon.Hash) []byte {
	return append(ethHashKeyPrefix, hash[:]...)
}

func receiptKey(txID ids.ID) []byte {
	return append(ethReceiptKeyPrefix, txID[:]...)
}

func rlpKey(txID ids.ID) []byte {
	return append(ethRLPKeyPrefix, txID[:]...)
}

func hexUint(v uint64) string {
	return fmt.Sprintf("0x%x", v)
}

// ethCallArgs is the eth_call/eth_estimateGas call object. Calldata arrives
// as "input" from geth-derived clients and as "data" from older ones.
type ethCallArgs struct {
	From  *ethcommon.Address `json:"from"`
	To    *ethcommon.Address `json:"to"`
	Value *hexBig            `json:"value"`
	Data  hexBytes           `json:"data"`
	Input hexBytes           `json:"input"`
}

// hexBig json-unmarshals a "0x..." quantity.
type hexBig big.Int

func (h *hexBig) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	v, ok := new(big.Int).SetString(strings.TrimPrefix(s, "0x"), 16)
	if !ok {
		return fmt.Errorf("bad quantity %q", s)
	}
	*h = hexBig(*v)
	return nil
}

func (h *hexBig) toInt() *big.Int {
	return (*big.Int)(h)
}

func (c *ethCallArgs) calldata() []byte {
	if len(c.Input) != 0 {
		return c.Input
	}
	return c.Data
}

// hexBytes json-unmarshals a "0x..." string.
type hexBytes []byte

func (h *hexBytes) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*h = ethcommon.FromHex(s)
	return nil
}

func parseParam(params []json.RawMessage, i int, v any) error {
	if i >= len(params) {
		return fmt.Errorf("missing param %d", i)
	}
	return json.Unmarshal(params[i], v)
}
