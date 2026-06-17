// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sidecar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/warp/external"
)

var (
	ErrEmptyJustification = errors.New("justification is empty: expected Solana transaction signature")
	ErrHTTPStatus         = errors.New("unexpected HTTP status from RPC")
	ErrHTTPRequest        = errors.New("RPC HTTP request failed")
	ErrDecodeResponse     = errors.New("failed to decode RPC response")
	ErrRPCError           = errors.New("RPC error")
)

var _ ValidationRule = (*SolanaRPCValidationRule)(nil)

// SolanaRPCValidationRule verifies an external event by querying a Solana
// JSON-RPC endpoint. It checks that the transaction identified by the event's
// justification exists, was included at the slot claimed in
// ExternalMessage.SourceBlockHeight, and that the transaction succeeded
// (meta.err == null).
//
// The justification field of the ExternalEvent must contain the UTF-8 bytes of
// the base58-encoded Solana transaction signature.
//
// Commitment controls the finality guarantee. Use "finalized" for production:
// the RPC returns null for any transaction whose block has not yet reached
// finality, so a non-null response implicitly confirms finality.
//
// All major Solana RPC providers (Helius, Alchemy, Triton, QuickNode) expose
// the same JSON-RPC 2.0 protocol, so this rule works with any of them — the
// only difference between providers is the URL.
type SolanaRPCValidationRule struct {
	RPCURL     string
	Commitment string // "finalized" | "confirmed"
	client     *http.Client
}

// NewSolanaRPCValidationRule creates a SolanaRPCValidationRule with a default HTTP client.
func NewSolanaRPCValidationRule(rpcURL, commitment string) *SolanaRPCValidationRule {
	return &SolanaRPCValidationRule{
		RPCURL:     rpcURL,
		Commitment: commitment,
		client:     &http.Client{},
	}
}

// newSolanaRPCValidationRuleWithClient creates a SolanaRPCValidationRule with
// the supplied HTTP client. Used in tests to inject an httptest.Server.
func newSolanaRPCValidationRuleWithClient(rpcURL, commitment string, client *http.Client) *SolanaRPCValidationRule {
	return &SolanaRPCValidationRule{
		RPCURL:     rpcURL,
		Commitment: commitment,
		client:     client,
	}
}

func (r *SolanaRPCValidationRule) Validate(ctx context.Context, event *external.ExternalEvent) (bool, error) {
	txSig := string(event.Justification)
	if txSig == "" {
		return false, ErrEmptyJustification
	}

	reqBody := solanaRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "getTransaction",
		Params: []interface{}{
			txSig,
			map[string]interface{}{
				"commitment":                     r.Commitment,
				"encoding":                       "json",
				"maxSupportedTransactionVersion": 0,
			},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return false, fmt.Errorf("marshal RPC request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, r.RPCURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return false, fmt.Errorf("create HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(httpReq)
	if err != nil {
		return false, fmt.Errorf("%w: %w", ErrHTTPRequest, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("%w: HTTP %d", ErrHTTPStatus, resp.StatusCode)
	}

	var rpcResp solanaGetTransactionResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return false, fmt.Errorf("%w: %w", ErrDecodeResponse, err)
	}

	if rpcResp.RPCError != nil {
		return false, fmt.Errorf("%w: code %d: %s", ErrRPCError, rpcResp.RPCError.Code, rpcResp.RPCError.Message)
	}

	// Null result: transaction not found or not yet at the requested
	// commitment level.
	if rpcResp.Result == nil {
		return false, nil
	}

	if rpcResp.Result.Slot != event.Message.SourceBlockHeight {
		return false, nil
	}

	// A non-nil Meta.Err means the transaction failed on-chain.
	if rpcResp.Result.Meta == nil || rpcResp.Result.Meta.Err != nil {
		return false, nil
	}

	return true, nil
}

// solanaRPCRequest is the JSON-RPC 2.0 request envelope for Solana.
type solanaRPCRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int           `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

type solanaGetTransactionResponse struct {
	JSONRPC  string                   `json:"jsonrpc"`
	Result   *solanaTransactionResult `json:"result"`
	RPCError *solanaRPCError          `json:"error"`
	ID       int                      `json:"id"`
}

type solanaTransactionResult struct {
	Slot      uint64            `json:"slot"`
	Meta      *solanaMetaResult `json:"meta"`
	BlockTime *int64            `json:"blockTime"`
}

type solanaMetaResult struct {
	// Err is null on success or a non-null value (string or object) on failure.
	Err         interface{} `json:"err"`
	LogMessages []string    `json:"logMessages"`
}

type solanaRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
