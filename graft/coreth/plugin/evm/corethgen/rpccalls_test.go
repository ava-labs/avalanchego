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

	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/synchronoustest"
)

// recordRPCCalls records coreth's response to every call in
// [generator.rpcRequests].
func (g *generator) recordRPCCalls(t *testing.T) {
	t.Helper()

	handler := g.rpcHandler(t)
	for _, req := range g.rpcRequests(t) {
		g.fixture.RPCCalls = append(g.fixture.RPCCalls, req.serve(t, handler))
	}
}

// rpcEndpointPath is the path coreth's VM mounts its JSON-RPC handler at.
const rpcEndpointPath = "/rpc"

func (g *generator) rpcHandler(t *testing.T) http.Handler {
	t.Helper()

	handlers, err := g.vm.CreateHandlers(t.Context())
	require.NoError(t, err, "vm.CreateHandlers()")
	handler, ok := handlers[rpcEndpointPath]
	require.Truef(t, ok, "VM serves a handler at %s", rpcEndpointPath)
	return handler
}

// rpcRequests returns every call to record. It is derived from
// [synchronoustest.Fixture.Blocks] and [generator.watchedAddresses], so adding
// a block or a watched account widens the coverage rather than leaving it
// stale.
//
// eth_getBlockByNumber and eth_getBlockByHash are left out on purpose, because
// SAE doesn't support the totalDifficulty field.
//
// eth_feeHistory is left out on purpose too, as it does not matter for
// historical blocks.
func (g *generator) rpcRequests(t *testing.T) []rpcRequest {
	t.Helper()

	// logFilter is the filter object of eth_getLogs. Every field is optional
	// and the zero value matches every log on the chain.
	type logFilter struct {
		FromBlock *hexutil.Uint64  `json:"fromBlock,omitempty"`
		ToBlock   *hexutil.Uint64  `json:"toBlock,omitempty"`
		BlockHash *common.Hash     `json:"blockHash,omitempty"`
		Addresses []common.Address `json:"address,omitempty"`
	}

	var reqs []rpcRequest
	for _, b := range g.fixture.Blocks {
		at := hexutil.Uint64(b.Number)
		block := fmt.Sprintf("block_%02d_%s", b.Number, b.Fork)

		for _, addr := range g.watchedAddresses() {
			acc := fmt.Sprintf("%s/%s", block, addr)
			// slot0 is the counter contract's only storage slot, and is zero in
			// every other watched account.
			var slot0 common.Hash
			reqs = append(reqs,
				newRPCRequest(acc+"/eth_getBalance", "eth_getBalance", addr, at),
				newRPCRequest(acc+"/eth_getTransactionCount", "eth_getTransactionCount", addr, at),
				newRPCRequest(acc+"/eth_getCode", "eth_getCode", addr, at),
				newRPCRequest(acc+"/eth_getStorageAt", "eth_getStorageAt", addr, slot0, at),
				newRPCRequest(acc+"/eth_getProof", "eth_getProof", addr, []common.Hash{slot0}, at),
			)
		}

		// Any non-empty call data makes the counter contract return slot 0
		// rather than increment it.
		type callArgs struct {
			To   common.Address `json:"to"`
			Data hexutil.Bytes  `json:"data"`
		}
		readCounter := callArgs{
			To:   g.counter,
			Data: hexutil.Bytes{1},
		}
		reqs = append(reqs,
			newRPCRequest(block+"/eth_call", "eth_call", readCounter, at),
			newRPCRequest(block+"/eth_callDetailed", "eth_callDetailed", readCounter, at),
			newRPCRequest(block+"/eth_getBlockReceipts", "eth_getBlockReceipts", at),
			newRPCRequest(block+"/eth_getLogs", "eth_getLogs", logFilter{
				FromBlock: &at,
				ToBlock:   &at,
			}),
			newRPCRequest(block+"/debug_intermediateRoots", "debug_intermediateRoots", b.Hash),
		)

		type traceConfig struct {
			Tracer       string          `json:"tracer"`
			TracerConfig json.RawMessage `json:"tracerConfig,omitempty"`
		}
		var (
			// The callTracer reports each transaction's tree of EVM calls.
			callTracer = traceConfig{Tracer: "callTracer"}
			// The prestateTracer reports the state each transaction read, or
			// the state changes it made in diff mode. The prestates are what
			// pin the replay's intra-block ordering and its base fee, because
			// they show the coinbase accruing each transaction's fees in turn.
			prestateTracer     = traceConfig{Tracer: "prestateTracer"}
			prestateDiffTracer = traceConfig{
				Tracer:       "prestateTracer",
				TracerConfig: json.RawMessage(`{"diffMode":true}`),
			}
		)

		// The three debug_traceBlock* methods differ in how they address the
		// block, by number, by hash, and by its RLP encoding. Each gets both
		// tracers because the RLP form takes its own path to the executed base
		// fee, which only the prestate reveals. Genesis fails to trace, in
		// coreth and in any replaying VM, and that failure is recorded like any
		// response.
		for _, tc := range []struct {
			name   string
			config traceConfig
		}{
			{"", callTracer},
			{"_prestate", prestateTracer},
		} {
			reqs = append(reqs,
				newRPCRequest(block+"/debug_traceBlockByNumber"+tc.name, "debug_traceBlockByNumber", at, tc.config),
				newRPCRequest(block+"/debug_traceBlockByHash"+tc.name, "debug_traceBlockByHash", b.Hash, tc.config),
				newRPCRequest(block+"/debug_traceBlock"+tc.name, "debug_traceBlock", b.RLP, tc.config),
			)
		}

		for i, tx := range b.EthBlock(t).Transactions() {
			txn := fmt.Sprintf("%s/tx_%d", block, i)
			reqs = append(reqs,
				newRPCRequest(txn+"/eth_getTransactionReceipt", "eth_getTransactionReceipt", tx.Hash()),
				newRPCRequest(txn+"/eth_getTransactionByHash", "eth_getTransactionByHash", tx.Hash()),
				newRPCRequest(txn+"/debug_traceTransaction", "debug_traceTransaction", tx.Hash(), callTracer),
				newRPCRequest(txn+"/debug_traceTransaction_prestate", "debug_traceTransaction", tx.Hash(), prestateTracer),
				newRPCRequest(txn+"/debug_traceTransaction_poststate", "debug_traceTransaction", tx.Hash(), prestateDiffTracer),
			)
		}
	}

	// Whole-chain log queries, which resolve a block range rather than the
	// single height the per-block queries above pin.
	var (
		genesis   = hexutil.Uint64(0)
		tip       = hexutil.Uint64(g.tip())
		warpBlock = g.fixture.Blocks[sendWarpMessageBlock].Hash
	)
	return append(reqs,
		newRPCRequest("chain/eth_getLogs_full_range", "eth_getLogs", logFilter{FromBlock: &genesis, ToBlock: &tip}),
		newRPCRequest("chain/eth_getLogs_by_block_hash", "eth_getLogs", logFilter{BlockHash: &warpBlock}),
		newRPCRequest("chain/eth_getLogs_by_address", "eth_getLogs", logFilter{
			FromBlock: &genesis,
			ToBlock:   &tip,
			Addresses: []common.Address{warp.ContractAddress},
		}),
	)
}

// An rpcRequest is a single call to record.
type rpcRequest struct {
	name   string
	method string
	params []any
}

func newRPCRequest(name, method string, params ...any) rpcRequest {
	return rpcRequest{name: name, method: method, params: params}
}

// serve makes the request against handler and returns the recorded call.
func (r rpcRequest) serve(t *testing.T, handler http.Handler) synchronoustest.RPCCall {
	t.Helper()

	params := make([]json.RawMessage, len(r.params))
	for i, p := range r.params {
		raw, err := json.Marshal(p)
		require.NoErrorf(t, err, "json.Marshal(%s param %d)", r.name, i)
		params[i] = raw
	}

	type jsonRPCRequest struct {
		Version string            `json:"jsonrpc"`
		ID      int               `json:"id"`
		Method  string            `json:"method"`
		Params  []json.RawMessage `json:"params"`
	}
	body, err := json.Marshal(jsonRPCRequest{
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

	type jsonRPCError struct {
		Message string `json:"message"`
	}
	type jsonRPCResponse struct {
		Result json.RawMessage `json:"result"`
		Error  *jsonRPCError   `json:"error"`
	}
	var resp jsonRPCResponse
	require.NoErrorf(t, json.Unmarshal(rec.Body.Bytes(), &resp), "unmarshalling %s response %s", r.name, rec.Body)

	call := synchronoustest.RPCCall{
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
