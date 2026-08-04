// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/math"
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

	// indexDB persists eth hash -> txID and txID -> inclusion block so
	// receipts survive restarts. Node-local, not consensus state.
	indexDB database.Database
}

var (
	ethIndexPrefix = []byte("ethTxIndex")

	// index key namespaces
	ethHashKeyPrefix    = []byte("h") // eth tx hash -> platform txID
	ethReceiptKeyPrefix = []byte("b") // platform txID -> receipt record
	ethRLPKeyPrefix     = []byte("r") // platform txID -> raw eth tx RLP
	ethWatermarkKey     = []byte("w") // last scanned block height
)

func newEthAPI(vm *VM) *ethAPI {
	return &ethAPI{
		vm:      vm,
		indexDB: prefixdb.New(ethIndexPrefix, vm.db),
	}
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
		return hexUint(txs.EthRLPChainID), nil

	case "net_version":
		return fmt.Sprintf("%d", txs.EthRLPChainID), nil

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
		var call struct {
			To   *ethcommon.Address `json:"to"`
			Data hexBytes           `json:"data"`
		}
		if err := parseParam(req.Params, 0, &call); err != nil {
			return nil, err
		}
		return a.ethCall(call.To, call.Data)

	case "eth_getLogs":
		var filter logFilter
		if err := parseParam(req.Params, 0, &filter); err != nil {
			return nil, err
		}
		return a.getLogs(&filter)

	case "eth_estimateGas":
		// Exact: complexity is defined from semantic fields only, so the gas
		// a call object implies equals the gas its signed tx will be charged.
		var call struct {
			Data hexBytes `json:"data"`
		}
		if err := parseParam(req.Params, 0, &call); err != nil {
			return nil, err
		}
		complexity := fee.EthRLPTxComplexity(len(call.Data))
		txGas, err := complexity.ToGas(a.vm.DynamicFeeConfig.Weights)
		if err != nil {
			return nil, err
		}
		return hexUint(uint64(txGas)), nil

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

	hash := unsigned.Parsed.Hash()
	txID := tx.ID()
	if err := errors.Join(
		a.indexDB.Put(hashKey(hash), txID[:]),
		a.indexDB.Put(rlpKey(txID), raw),
	); err != nil {
		return nil, err
	}

	if err := a.vm.issueTxFromRPC(tx); err != nil {
		return nil, err
	}
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
	txIDBytes, err := a.indexDB.Get(hashKey(hash))
	if err == database.ErrNotFound {
		return nil, nil // unknown to this node
	}
	if err != nil {
		return nil, err
	}
	txID, err := ids.ToID(txIDBytes)
	if err != nil {
		return nil, err
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
	if err != nil && err != database.ErrNotFound {
		return err
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
			complexity := fee.EthRLPTxComplexity(len(unsigned.Parsed.Data()))
			txGas, err := complexity.ToGas(a.vm.DynamicFeeConfig.Weights)
			if err != nil {
				return err
			}
			txID := tx.ID()
			record := ethReceiptRecord{
				height:   h,
				blkID:    blkID,
				gasUsed:  uint64(txGas),
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

// liquidBalance sums the spendable single-key AVAX UTXOs owned by [addr],
// mirroring the executor's auto-selection filter and its input bound.
func (a *ethAPI) liquidBalance(addr ids.ShortID) (uint64, error) {
	utxoIDs, err := a.vm.state.UTXOIDs(addr.Bytes(), ids.Empty, txs.MaxEthRLPTxInputs)
	if err != nil {
		return 0, err
	}
	chainTime := uint64(a.vm.state.GetTimestamp().Unix())
	var balance uint64
	for _, utxoID := range utxoIDs {
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
		balance, err = math.Add(balance, out.Amt)
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
