// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	ethcommon "github.com/ava-labs/libevm/common"
)

// ethAPI is a minimal Ethereum JSON-RPC facade over the P-chain so that stock
// EVM tooling can read balances and issue EthRLPTxs. Single requests only.
// ponytail: prototype; no batch requests, no logs, no historical blocks.
type ethAPI struct {
	vm *VM

	lock       sync.Mutex
	txHashToID map[ethcommon.Hash]ids.ID
}

// ethGasPriceWei prices one P-chain gas unit at 1 nAVAX, expressed in wei so
// wallet cost math (gas * price, 18 decimals) is exact.
var ethGasPriceWei = big.NewInt(1_000_000_000)

func newEthAPI(vm *VM) *ethAPI {
	return &ethAPI{
		vm:         vm,
		txHashToID: make(map[ethcommon.Hash]ids.ID),
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

	case "eth_gasPrice", "eth_maxPriorityFeePerGas":
		return "0x" + ethGasPriceWei.Text(16), nil

	case "eth_estimateGas":
		// P-chain fees are exact pre-execution; the prototype returns a flat
		// budget that covers a transfer with change at 1 nAVAX per gas.
		return hexUint(500_000), nil

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
	if err := a.vm.issueTxFromRPC(tx); err != nil {
		return nil, err
	}

	hash := unsigned.Parsed.Hash()
	a.lock.Lock()
	a.txHashToID[hash] = tx.ID()
	a.lock.Unlock()
	return hash.Hex(), nil
}

func (a *ethAPI) getTransactionReceipt(hash ethcommon.Hash) (any, error) {
	a.lock.Lock()
	txID, ok := a.txHashToID[hash]
	a.lock.Unlock()
	if !ok {
		return nil, nil
	}

	tx, txStatus, err := a.vm.state.GetTx(txID)
	if err != nil || txStatus != status.Committed {
		return nil, nil // still pending (or dropped): no receipt yet
	}
	unsigned, ok := tx.Unsigned.(*txs.EthRLPTx)
	if !ok {
		return nil, nil
	}
	if err := unsigned.SyntacticVerify(a.vm.ctx); err != nil {
		return nil, err
	}

	height, err := a.lastAcceptedHeight()
	if err != nil {
		return nil, err
	}
	blockID := a.vm.state.GetLastAccepted()
	sender := ethcommon.Address(unsigned.Sender)
	recipient := ethcommon.Address(unsigned.Recipient)
	return map[string]any{
		"transactionHash": hash.Hex(),
		"transactionIndex": "0x0",
		// ponytail: the receipt pins the last accepted block, not the true
		// inclusion block; good enough for tooling that polls for acceptance.
		"blockHash":         "0x" + hex.EncodeToString(blockID[:]),
		"blockNumber":       hexUint(height),
		"from":              sender.Hex(),
		"to":                recipient.Hex(),
		"status":            "0x1",
		"type":              "0x2",
		"gasUsed":           hexUint(unsigned.Parsed.Gas()),
		"cumulativeGasUsed": hexUint(unsigned.Parsed.Gas()),
		"effectiveGasPrice": "0x" + ethGasPriceWei.Text(16),
		"contractAddress":   nil,
		"logs":              []any{},
		"logsBloom":         "0x" + fmt.Sprintf("%0512x", 0),
	}, nil
}

// liquidBalance sums the spendable single-key AVAX UTXOs owned by [addr],
// mirroring the executor's auto-selection filter.
func (a *ethAPI) liquidBalance(addr ids.ShortID) (uint64, error) {
	utxoIDs, err := a.vm.state.UTXOIDs(addr.Bytes(), ids.Empty, 1024)
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

func (a *ethAPI) lastAcceptedHeight() (uint64, error) {
	blk, err := a.vm.state.GetStatelessBlock(a.vm.state.GetLastAccepted())
	if err != nil {
		return 0, err
	}
	return blk.Height(), nil
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
