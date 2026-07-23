// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package relayer

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethclient"
)

// WarpPrecompileAddr is the warp precompile address on subnet-evm chains.
var WarpPrecompileAddr = common.HexToAddress("0x0200000000000000000000000000000000000005")

// FetchLog fetches the receipt of txHash over raw JSON-RPC and returns the
// block height and the data of the first log emitted by contract.
func FetchLog(ctx context.Context, rpcURL, txHash string, contract common.Address) (uint64, []byte, error) {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1,
		"method": "eth_getTransactionReceipt",
		"params": []any{txHash},
	})
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rpcURL, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	var envelope struct {
		Result *struct {
			BlockNumber string `json:"blockNumber"`
			Logs        []struct {
				Address string `json:"address"`
				Data    string `json:"data"`
			} `json:"logs"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return 0, nil, err
	}
	if envelope.Result == nil {
		return 0, nil, fmt.Errorf("transaction %s not found", txHash)
	}
	height, err := strconv.ParseUint(strings.TrimPrefix(envelope.Result.BlockNumber, "0x"), 16, 64)
	if err != nil {
		return 0, nil, err
	}
	for _, l := range envelope.Result.Logs {
		if strings.EqualFold(l.Address, contract.Hex()) {
			data, err := hex.DecodeString(strings.TrimPrefix(l.Data, "0x"))
			if err != nil {
				return 0, nil, err
			}
			return height, data, nil
		}
	}
	return 0, nil, fmt.Errorf("no log from %s in transaction", contract)
}

// WaitReceipt polls for the receipt of h until it lands or ctx expires.
func WaitReceipt(ctx context.Context, client *ethclient.Client, h common.Hash) (*types.Receipt, error) {
	for {
		r, err := client.TransactionReceipt(ctx, h)
		if err == nil {
			return r, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// BuildPredicate packs a signed warp message into the access-list predicate
// the warp precompile expects: message || 0xff, zero-padded to a 32-byte
// multiple, chunked into storage keys on the precompile address.
func BuildPredicate(warpMessage []byte) types.AccessList {
	predicate := append(bytes.Clone(warpMessage), 0xff)
	if rem := len(predicate) % 32; rem != 0 {
		predicate = append(predicate, make([]byte, 32-rem)...)
	}
	storageKeys := make([]common.Hash, 0, len(predicate)/32)
	for i := 0; i < len(predicate); i += 32 {
		storageKeys = append(storageKeys, common.BytesToHash(predicate[i:i+32]))
	}
	return types.AccessList{{Address: WarpPrecompileAddr, StorageKeys: storageKeys}}
}
