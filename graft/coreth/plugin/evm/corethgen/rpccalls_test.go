// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package corethgen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/stretchr/testify/require"

	corethwarp "github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/corethtest"
)

// rpcEndpointPath is the path coreth's VM mounts its JSON-RPC handler at.
const rpcEndpointPath = "/rpc"

// traceConfig selects the tracer of the debug_trace* methods.
type traceConfig struct {
	Tracer       string          `json:"tracer"`
	TracerConfig json.RawMessage `json:"tracerConfig,omitempty"`
}

// Tracer configs recorded for the debug_trace* methods. The callTracer reports
// each transaction's tree of EVM calls; the prestateTracer reports the state
// each transaction read, or the state changes it made in diff mode. The
// prestates are what pin the replay's intra-block ordering and its base fee,
// because they show the coinbase accruing each transaction's fees in turn.
var (
	callTracer         = traceConfig{Tracer: "callTracer"}
	prestateTracer     = traceConfig{Tracer: "prestateTracer"}
	prestateDiffTracer = traceConfig{
		Tracer:       "prestateTracer",
		TracerConfig: json.RawMessage(`{"diffMode":true}`),
	}
)

// recordRPCCalls records coreth's response to every call in
// [generator.rpcCallMatrix].
//
// MUST run before the VM shuts down. MUST NOT be reimplemented against a
// consuming VM, for the reason given on [RPCCall].
func (g *generator) recordRPCCalls(t *testing.T) {
	t.Helper()

	handler := g.rpcHandler(t)
	for _, req := range g.rpcCallMatrix(t) {
		g.fixture.RPCCalls = append(g.fixture.RPCCalls, req.serve(t, handler))
	}
}

func (g *generator) rpcHandler(t *testing.T) http.Handler {
	t.Helper()

	handlers, err := g.vm.CreateHandlers(t.Context())
	require.NoError(t, err, "vm.CreateHandlers()")
	handler, ok := handlers[rpcEndpointPath]
	require.Truef(t, ok, "VM serves a handler at %s", rpcEndpointPath)
	return handler
}

// An rpcRequest is a single call to record.
type rpcRequest struct {
	name   string
	method string
	params []any
}

// callArgs is the transaction-call object of eth_call and eth_callDetailed.
type callArgs struct {
	To   common.Address `json:"to"`
	Data hexutil.Bytes  `json:"data"`
}

// logFilter is the filter object of eth_getLogs. Every field is optional and
// the zero value matches every log on the chain.
type logFilter struct {
	FromBlock *hexutil.Uint64  `json:"fromBlock,omitempty"`
	ToBlock   *hexutil.Uint64  `json:"toBlock,omitempty"`
	BlockHash *common.Hash     `json:"blockHash,omitempty"`
	Addresses []common.Address `json:"address,omitempty"`
}

// sendWarpMessageBlock's sendWarpMessage logs the warp precompile's
// SendWarpMessage event.
const sendWarpMessageBlock uint64 = 16

// rpcCallMatrix returns every call to record. It is derived from
// [corethtest.Fixture.Blocks] and [generator.watchedAddresses], so adding a
// block or a watched account widens the coverage rather than leaving it stale.
//
// Four methods are left out on purpose, because Coreth's answer is the wrong
// reference for them. eth_getBlockByNumber and eth_getBlockByHash report a
// totalDifficulty that a successor VM has no reason to maintain, and every
// other field of those responses already agrees. eth_feeHistory estimates the
// next block's base fee using whichever gas-price implementation the VM has.
// debug_traceChain streams over a subscription, so it has no single response to
// record.
func (g *generator) rpcCallMatrix(t *testing.T) []rpcRequest {
	t.Helper()

	var reqs []rpcRequest
	add := func(name, method string, params ...any) {
		reqs = append(reqs, rpcRequest{name: name, method: method, params: params})
	}

	// slot0 is the counter contract's only storage slot, and is zero in every
	// other watched account.
	var slot0 common.Hash
	// Any non-empty call data makes the counter contract return slot 0 rather
	// than increment it.
	readCounter := callArgs{To: g.counter, Data: hexutil.Bytes{1}}

	for _, b := range g.fixture.Blocks {
		at := hexutil.Uint64(b.Number)
		block := fmt.Sprintf("block_%02d", b.Number)

		for _, addr := range g.watchedAddresses() {
			acc := fmt.Sprintf("%s/%s", block, addr)
			add(acc+"/eth_getBalance", "eth_getBalance", addr, at)
			add(acc+"/eth_getTransactionCount", "eth_getTransactionCount", addr, at)
			add(acc+"/eth_getCode", "eth_getCode", addr, at)
			add(acc+"/eth_getStorageAt", "eth_getStorageAt", addr, slot0, at)
			add(acc+"/eth_getProof", "eth_getProof", addr, []common.Hash{slot0}, at)
		}

		add(block+"/eth_call", "eth_call", readCounter, at)
		add(block+"/eth_callDetailed", "eth_callDetailed", readCounter, at)
		add(block+"/eth_getBlockReceipts", "eth_getBlockReceipts", at)
		// Pins the per-block logs, including the blocks that emit none.
		add(block+"/eth_getLogs", "eth_getLogs", logFilter{FromBlock: &at, ToBlock: &at})
		// The state root after each transaction, which pins replay ordering.
		// Genesis is not traceable, so this records coreth's error for it.
		add(block+"/debug_intermediateRoots", "debug_intermediateRoots", b.Hash)

		// The three debug_traceBlock* methods differ in how they address the
		// block, by number, by hash, and by its RLP encoding. Each gets both
		// tracers because the RLP form takes its own path to the executed base
		// fee, which only the prestate reveals. Genesis and the blocks carrying
		// a functional nativeAssetCall fail to trace, in coreth and in any
		// replaying VM, and those failures are recorded like any response.
		for _, tc := range []struct {
			name   string
			config traceConfig
		}{
			{"", callTracer},
			{"_prestate", prestateTracer},
		} {
			add(block+"/debug_traceBlockByNumber"+tc.name, "debug_traceBlockByNumber", at, tc.config)
			add(block+"/debug_traceBlockByHash"+tc.name, "debug_traceBlockByHash", b.Hash, tc.config)
			add(block+"/debug_traceBlock"+tc.name, "debug_traceBlock", b.RLP, tc.config)
		}

		for i, tx := range b.EthBlock(t).Transactions() {
			txn := fmt.Sprintf("%s/tx_%d", block, i)
			add(txn+"/eth_getTransactionReceipt", "eth_getTransactionReceipt", tx.Hash())
			add(txn+"/eth_getTransactionByHash", "eth_getTransactionByHash", tx.Hash())
			add(txn+"/debug_traceTransaction", "debug_traceTransaction", tx.Hash(), callTracer)
			add(txn+"/debug_traceTransaction_prestate", "debug_traceTransaction", tx.Hash(), prestateTracer)
			add(txn+"/debug_traceTransaction_poststate", "debug_traceTransaction", tx.Hash(), prestateDiffTracer)
		}
	}

	// Whole-chain log queries, which resolve a block range rather than the
	// single height the per-block queries above pin.
	var (
		genesis   = hexutil.Uint64(0)
		tip       = hexutil.Uint64(g.tip())
		warpBlock = g.fixture.Blocks[sendWarpMessageBlock].Hash
	)
	add("chain/eth_getLogs_full_range", "eth_getLogs", logFilter{FromBlock: &genesis, ToBlock: &tip})
	add("chain/eth_getLogs_by_block_hash", "eth_getLogs", logFilter{BlockHash: &warpBlock})
	add("chain/eth_getLogs_by_address", "eth_getLogs", logFilter{
		FromBlock: &genesis,
		ToBlock:   &tip,
		Addresses: []common.Address{corethwarp.ContractAddress},
	})

	return reqs
}

// serve makes the request against handler and returns the recorded call.
func (r rpcRequest) serve(t *testing.T, handler http.Handler) corethtest.RPCCall {
	t.Helper()

	params := make([]json.RawMessage, len(r.params))
	for i, p := range r.params {
		raw, err := json.Marshal(p)
		require.NoErrorf(t, err, "json.Marshal(%s param %d)", r.name, i)
		params[i] = raw
	}

	body, err := json.Marshal(struct {
		Version string            `json:"jsonrpc"`
		ID      int               `json:"id"`
		Method  string            `json:"method"`
		Params  []json.RawMessage `json:"params"`
	}{
		Version: "2.0",
		ID:      1,
		Method:  r.method,
		Params:  params,
	})
	require.NoErrorf(t, err, "json.Marshal(%s request)", r.name)

	httpReq := httptest.NewRequest(http.MethodPost, rpcEndpointPath, bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httpReq)
	require.Equalf(t, http.StatusOK, rec.Code, "%s HTTP status, body %s", r.name, rec.Body)

	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoErrorf(t, json.Unmarshal(rec.Body.Bytes(), &resp), "unmarshalling %s response %s", r.name, rec.Body)

	call := corethtest.RPCCall{
		Name:   r.name,
		Method: r.method,
		Params: params,
	}
	switch {
	case resp.Error != nil:
		call.Error = resp.Error.Message
	default:
		require.NotEmptyf(t, resp.Result, "%s returned neither a result nor an error", r.name)
		call.Result = resp.Result
	}
	return call
}
