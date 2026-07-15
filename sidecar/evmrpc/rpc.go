// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
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

// txReceipt mirrors the fields of eth_getTransactionReceipt this verifier
// needs. Quantities are 0x-prefixed hex per the Ethereum JSON-RPC spec.
type txReceipt struct {
	Status      string  `json:"status"`
	BlockNumber string  `json:"blockNumber"`
	Logs        []txLog `json:"logs"`
}

type txLog struct {
	Address string   `json:"address"`
	Topics  []string `json:"topics"`
	Data    string   `json:"data"`
}

type blockHeader struct {
	Number string `json:"number"`
}

// rpcClient is the interface EVMVerifier uses to query the source chain.
// The only production implementation is evmClient; tests inject a stub.
type rpcClient interface {
	getTransactionReceipt(ctx context.Context, txHash string) (*txReceipt, error)
	getBlockNumberByTag(ctx context.Context, tag string) (uint64, error)
}

type evmClient struct {
	rpcURL     string
	httpClient *http.Client
}

func newEVMClient(rpcURL string, httpClient *http.Client) *evmClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &evmClient{rpcURL: rpcURL, httpClient: httpClient}
}

func (c *evmClient) call(ctx context.Context, method string, params []any, result any) error {
	body, err := json.Marshal(rpcEnvelope{
		JSONRPC: "2.0",
		ID:      1,
		Method:  method,
		Params:  params,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal %s request: %w", method, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.rpcURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to build %s request: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s request failed: %w", method, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read %s response: %w", method, err)
	}
	var envelope rpcResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("failed to parse %s response: %w", method, err)
	}
	if envelope.Error != nil {
		return fmt.Errorf("%s RPC error %d: %s", method, envelope.Error.Code, envelope.Error.Message)
	}
	// A null result (e.g. unknown tx hash) unmarshals into the zero value;
	// callers distinguish it by checking for required fields.
	if bytes.Equal(envelope.Result, []byte("null")) {
		return nil
	}
	if err := json.Unmarshal(envelope.Result, result); err != nil {
		return fmt.Errorf("failed to parse %s result: %w", method, err)
	}
	return nil
}

func (c *evmClient) getTransactionReceipt(ctx context.Context, txHash string) (*txReceipt, error) {
	var receipt txReceipt
	if err := c.call(ctx, "eth_getTransactionReceipt", []any{txHash}, &receipt); err != nil {
		return nil, err
	}
	if receipt.BlockNumber == "" {
		return nil, nil // not found or pending
	}
	return &receipt, nil
}

func (c *evmClient) getBlockNumberByTag(ctx context.Context, tag string) (uint64, error) {
	var header blockHeader
	if err := c.call(ctx, "eth_getBlockByNumber", []any{tag, false}, &header); err != nil {
		return 0, err
	}
	if header.Number == "" {
		return 0, fmt.Errorf("no block returned for tag %q", tag)
	}
	return parseHexUint64(header.Number)
}

func parseHexUint64(s string) (uint64, error) {
	trimmed := strings.TrimPrefix(s, "0x")
	if trimmed == s {
		return 0, fmt.Errorf("quantity %q is not 0x-prefixed", s)
	}
	v, err := strconv.ParseUint(trimmed, 16, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse hex quantity %q: %w", s, err)
	}
	return v, nil
}
