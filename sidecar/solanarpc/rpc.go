// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type rpcEnvelope struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type txResult struct {
	Slot        uint64 `json:"slot"`
	Transaction txData `json:"transaction"`
}

type txData struct {
	Message txMessage `json:"message"`
}

type txMessage struct {
	AccountKeys  []string        `json:"accountKeys"`
	Instructions []txInstruction `json:"instructions"`
}

type txInstruction struct {
	ProgramIDIndex int    `json:"programIdIndex"`
	Data           string `json:"data"` // base58-encoded
}

type solanaClient struct {
	rpcURL     string
	httpClient *http.Client
}

func newSolanaClient(rpcURL string, httpClient *http.Client) *solanaClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &solanaClient{
		rpcURL:     rpcURL,
		httpClient: httpClient,
	}
}

// getTransaction fetches a Solana transaction by its base58-encoded signature.
// Returns (nil, nil) when the transaction is not found (result is JSON null).
func (c *solanaClient) getTransaction(ctx context.Context, sig string) (*txResult, error) {
	req := rpcEnvelope{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "getTransaction",
		Params: []any{
			sig,
			map[string]any{
				"encoding":                       "json",
				"maxSupportedTransactionVersion": 0,
			},
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal RPC request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.rpcURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to build RPC HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("RPC request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read RPC response body: %w", err)
	}

	var rpcResp rpcResponse
	if err := json.Unmarshal(respBytes, &rpcResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal RPC response: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	// null result means transaction not found
	if string(rpcResp.Result) == "null" {
		return nil, nil
	}

	var tx txResult
	if err := json.Unmarshal(rpcResp.Result, &tx); err != nil {
		return nil, fmt.Errorf("failed to unmarshal transaction result: %w", err)
	}

	return &tx, nil
}
