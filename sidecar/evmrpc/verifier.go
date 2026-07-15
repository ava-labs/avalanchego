// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package evmrpc verifies OracleMessages against any Ethereum
// JSON-RPC-compatible chain (Besu, geth, public testnets, L2s). It is the
// second source type after solanarpc and follows the same contract: the
// sidecar binary treats the config as opaque bytes and this package is the
// authority on what fields are valid.
package evmrpc

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ava-labs/avalanchego/network/p2p/oracle"
)

const (
	// FinalityLatest treats inclusion as final. Correct for chains with
	// single-slot/instant finality such as QBFT and IBFT networks.
	FinalityLatest = "latest"
	// FinalityFinalized only attests to blocks at or below the chain's
	// `finalized` tag. Correct for post-merge Ethereum networks; note the
	// tag lags the tip by ~two epochs (~13 minutes) on mainnet and Sepolia.
	FinalityFinalized = "finalized"
	// FinalitySafe uses the `safe` tag, a weaker but faster Ethereum
	// finality signal.
	FinalitySafe = "safe"
)

// Config is the verifier's own configuration, parsed from the sidecar's
// --config-path file.
type Config struct {
	// RPCURL is the Ethereum JSON-RPC endpoint of the source chain.
	// Required: there is no sensible default chain to attest to.
	RPCURL string `json:"rpc_url"`
	// AllowedContracts is an optional list of contract addresses this
	// sidecar will attest to. Omit or leave empty to allow all contracts.
	// Comparison is case-insensitive.
	AllowedContracts []string `json:"allowed_contracts"`
	// Finality selects the rule for when an included transaction may be
	// attested: "latest" (default; inclusion is final — QBFT/IBFT),
	// "finalized", or "safe" (Ethereum finality tags).
	Finality string `json:"finality"`
}

// EVMVerifier verifies OracleMessages by querying an Ethereum JSON-RPC
// endpoint.
type EVMVerifier struct {
	client           rpcClient
	allowedContracts map[string]struct{} // lowercased; empty = allow all
	finality         string
}

// NewEVMVerifier parses configBytes as a JSON Config and constructs an
// EVMVerifier.
func NewEVMVerifier(configBytes []byte, httpClient *http.Client) (*EVMVerifier, error) {
	var cfg Config
	if len(configBytes) > 0 {
		if err := json.Unmarshal(configBytes, &cfg); err != nil {
			return nil, fmt.Errorf("invalid evm verifier config: %w", err)
		}
	}
	if cfg.RPCURL == "" {
		return nil, errors.New("evm verifier config: rpc_url must not be empty")
	}
	switch cfg.Finality {
	case "":
		cfg.Finality = FinalityLatest
	case FinalityLatest, FinalityFinalized, FinalitySafe:
	default:
		return nil, fmt.Errorf("evm verifier config: unknown finality %q", cfg.Finality)
	}
	allowed := make(map[string]struct{}, len(cfg.AllowedContracts))
	for _, a := range cfg.AllowedContracts {
		allowed[strings.ToLower(a)] = struct{}{}
	}
	return &EVMVerifier{
		client:           newEVMClient(cfg.RPCURL, httpClient),
		allowedContracts: allowed,
		finality:         cfg.Finality,
	}, nil
}

// ErrLogNotFound is returned by Verify when the transaction contains no log
// from msg.SourceAddress whose data matches msg.Payload.
var ErrLogNotFound = errors.New("no matching log found for contract")

// Verify checks that the given OracleMessage is backed by a real transaction
// on the source chain. The justification must be a raw 32-byte transaction
// hash. Verification requires that:
//
//  1. the transaction exists and succeeded (receipt status 1),
//  2. it was included at msg.SourceBlockHeight,
//  3. msg.SourceAddress is in the allowed contracts list (if configured),
//  4. a log emitted by msg.SourceAddress has data byte-equal to msg.Payload
//     (the EVM analog of solanarpc's instruction-data equality), and
//  5. the inclusion block satisfies the configured finality rule.
func (v *EVMVerifier) Verify(ctx context.Context, msg *oracle.OracleMessage, justification []byte) error {
	if len(justification) != 32 {
		return fmt.Errorf("justification must be a 32-byte transaction hash, got %d bytes", len(justification))
	}
	sourceAddr := strings.ToLower(msg.SourceAddress)
	if len(v.allowedContracts) > 0 {
		if _, ok := v.allowedContracts[sourceAddr]; !ok {
			return fmt.Errorf("source address %q is not in the allowed contracts list", msg.SourceAddress)
		}
	}

	txHash := "0x" + hex.EncodeToString(justification)
	receipt, err := v.client.getTransactionReceipt(ctx, txHash)
	if err != nil {
		return fmt.Errorf("%w: getTransactionReceipt RPC call failed: %w", oracle.ErrSourceUnavailable, err)
	}
	if receipt == nil {
		return fmt.Errorf("transaction not found for hash %s", txHash)
	}
	if receipt.Status != "0x1" {
		return fmt.Errorf("transaction %s did not succeed (status %s)", txHash, receipt.Status)
	}

	blockNumber, err := parseHexUint64(receipt.BlockNumber)
	if err != nil {
		return err
	}
	if blockNumber != msg.SourceBlockHeight {
		return fmt.Errorf("block height mismatch: got %d, want %d", blockNumber, msg.SourceBlockHeight)
	}

	if err := v.checkFinality(ctx, blockNumber); err != nil {
		return err
	}

	// Find a log emitted by the source contract whose data equals the
	// attested payload. Topic contents are intentionally not interpreted:
	// byte-exact data equality is the verification contract, and the
	// relayer must construct msg.Payload as the raw log data.
	for _, l := range receipt.Logs {
		if strings.ToLower(l.Address) != sourceAddr {
			continue
		}
		data, err := hexBytes(l.Data)
		if err != nil {
			return fmt.Errorf("failed to decode log data: %w", err)
		}
		if bytesEqual(data, msg.Payload) {
			return nil
		}
	}
	return fmt.Errorf("%w %q in transaction %s", ErrLogNotFound, msg.SourceAddress, txHash)
}

// checkFinality enforces the configured finality rule for a block at the
// given height. Under FinalityLatest inclusion is sufficient and no RPC call
// is made.
func (v *EVMVerifier) checkFinality(ctx context.Context, blockNumber uint64) error {
	if v.finality == FinalityLatest {
		return nil
	}
	tagHeight, err := v.client.getBlockNumberByTag(ctx, v.finality)
	if err != nil {
		return fmt.Errorf("%w: %s tag lookup failed: %w", oracle.ErrSourceUnavailable, v.finality, err)
	}
	if blockNumber > tagHeight {
		return fmt.Errorf("block %d is not yet %s (tag at %d)", blockNumber, v.finality, tagHeight)
	}
	return nil
}

func hexBytes(s string) ([]byte, error) {
	return hex.DecodeString(strings.TrimPrefix(s, "0x"))
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
