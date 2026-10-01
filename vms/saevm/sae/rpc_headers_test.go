// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"bytes"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/libevm/ethapi"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/rpc"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest/escrow"

	saerpc "github.com/ava-labs/avalanchego/vms/saevm/sae/rpc"
)

func TestResponseHeaders(t *testing.T) {
	echoReverter := common.Address{'e', 'c', 'h', 'o'}
	ctx, sut := newSUT(t, 1, options.Func[sutConfig](func(c *sutConfig) {
		c.genesis.Alloc[echoReverter] = types.Account{
			Code: saetest.Ops(
				vm.CALLDATASIZE, vm.PUSH0, vm.PUSH0, vm.CALLDATACOPY,
				vm.CALLDATASIZE, vm.PUSH0, vm.REVERT,
			),
			Balance: new(big.Int),
		}
	}))

	recipient := common.Address{'r', 'e', 'c', 'v'}
	escrowAddr := sut.deployEscrow(t)
	sut.depositToEscrow(t, escrowAddr, recipient, common.Big1)

	handlers, err := sut.CreateHandlers(ctx)
	require.NoErrorf(t, err, "%T.CreateHandlers()", sut)
	server := httptest.NewServer(handlers[rpcHTTPExtensionPath])
	t.Cleanup(server.Close)

	latest := rpc.LatestBlockNumber.String()
	balanceOf := ethapi.TransactionArgs{
		To:   &escrowAddr,
		Data: new(hexutil.Bytes(escrow.CallDataForBalance(recipient))),
	}
	revert := ethapi.TransactionArgs{
		To:   &echoReverter,
		Data: new(hexutil.Bytes{42}),
	}
	const (
		balanceOfGas = "23675"
		revertGas    = "21035"
		batchGas     = "44710" // balanceOfGas + revertGas

		batchWithErrorsGas = "65745" // balanceOfGas + 2*revertGas
	)

	type request struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id,omitempty"`
		Method  string `json:"method"`
		Params  []any  `json:"params"`
	}
	newReq := func(method string, params ...any) request {
		return request{JSONRPC: "2.0", ID: 1, Method: method, Params: params}
	}

	tests := []struct {
		name           string
		body           any
		wantGas        []string // nil if the header must be absent
		wantErrorCodes []string
	}{
		{
			name:    "eth_call",
			body:    newReq("eth_call", balanceOf, latest),
			wantGas: []string{balanceOfGas},
		},
		{
			name:    "eth_call_default_block",
			body:    newReq("eth_call", balanceOf),
			wantGas: []string{balanceOfGas},
		},
		{
			name:           "eth_call_revert",
			body:           newReq("eth_call", revert, latest),
			wantGas:        []string{revertGas},
			wantErrorCodes: []string{"3;count=1"},
		},
		{
			name: "batch",
			body: []request{
				newReq("eth_call", balanceOf, latest),
				newReq("eth_call", revert, latest),
				newReq("eth_blockNumber"),
			},
			wantGas:        []string{batchGas},
			wantErrorCodes: []string{"3;count=1"},
		},
		{
			name: "batch_with_errors",
			body: []request{
				newReq("eth_call", revert, latest),
				newReq("eth_call", balanceOf, latest),
				newReq("eth_unknownMethod"),
				newReq("eth_call", revert, latest),
			},
			wantGas:        []string{batchWithErrorsGas},
			wantErrorCodes: []string{"-32601;count=1, 3;count=2"},
		},
		{
			// Without an ID, the call runs but no response is written.
			name:    "notification",
			body:    request{JSONRPC: "2.0", Method: "eth_call", Params: []any{balanceOf, latest}},
			wantGas: []string{balanceOfGas},
		},
		{
			name: "no_calls",
			body: newReq("eth_blockNumber"),
		},
		{
			name:           "unknown_block",
			body:           newReq("eth_call", balanceOf, common.Hash{1}),
			wantErrorCodes: []string{"-32000;count=1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, bytes.NewReader(marshalJSON(t, tt.body)))
			require.NoError(t, err, "http.NewRequestWithContext()")
			req.Header.Set("Content-Type", "application/json")
			resp, err := server.Client().Do(req)
			require.NoErrorf(t, err, "%T.Do()", server.Client())
			defer resp.Body.Close()

			require.Equal(t, http.StatusOK, resp.StatusCode, "status code")
			require.Equalf(t, tt.wantGas, resp.Header[saerpc.GasUsedHeader], "%q header", saerpc.GasUsedHeader)
			require.Equalf(t, tt.wantErrorCodes, resp.Header[saerpc.ErrorCodesHeader], "%q header", saerpc.ErrorCodesHeader)
		})
	}
}
