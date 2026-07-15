// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Command sidecarcheck exercises a running oracle sidecar end to end against a
// real EVM transaction: it fetches the receipt, constructs the OracleMessage a
// relayer would construct (payload = raw log data from the given contract),
// and asks the sidecar to verify it over gRPC. With --tamper it corrupts the
// payload first, which a healthy sidecar must refuse.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/network/p2p/oracle"

	pb "github.com/ava-labs/avalanchego/proto/pb/oracle"
)

func main() {
	sidecarAddr := flag.String("sidecar", "127.0.0.1:7100", "oracle sidecar gRPC address")
	rpcURL := flag.String("rpc", "http://127.0.0.1:9545", "EVM JSON-RPC endpoint of the source chain")
	txHashHex := flag.String("tx", "", "transaction hash of the event-emitting tx (required)")
	contract := flag.String("contract", "", "address of the event-emitting contract (required)")
	tamper := flag.Bool("tamper", false, "corrupt the payload before asking; the sidecar must refuse")
	flag.Parse()

	if *txHashHex == "" || *contract == "" {
		log.Fatal("--tx and --contract are required")
	}
	txHash, err := hex.DecodeString(strings.TrimPrefix(*txHashHex, "0x"))
	if err != nil || len(txHash) != 32 {
		log.Fatalf("--tx must be a 32-byte hex hash: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Fetch the receipt and pull out the log — exactly what the relayer will do.
	height, payload, err := fetchLog(ctx, *rpcURL, *txHashHex, *contract)
	if err != nil {
		log.Fatalf("failed to fetch source event: %v", err)
	}
	fmt.Printf("source event: block %d, %d payload bytes from %s\n", height, len(payload), *contract)

	if *tamper {
		payload = bytes.Clone(payload)
		payload[len(payload)-1] ^= 0xff
		fmt.Println("TAMPER: corrupted the final payload byte — the sidecar must refuse this")
	}

	msg, err := oracle.NewOracleMessage(oracle.SourceTypeEVM, *contract, common.Address{}, height, 1, payload)
	if err != nil {
		log.Fatalf("failed to build OracleMessage: %v", err)
	}

	conn, err := grpc.NewClient(*sidecarAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("failed to dial sidecar: %v", err)
	}
	defer conn.Close()

	_, err = pb.NewOracleSidecarClient(conn).Verify(ctx, &pb.VerifyRequest{
		MessageBytes:  msg.Bytes(),
		Justification: txHash,
	})
	if err != nil {
		fmt.Printf("sidecar REFUSED to attest: %v\n", err)
		return
	}
	fmt.Println("sidecar ATTESTED: the event was independently verified on the source chain")
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

func fetchLog(ctx context.Context, rpcURL, txHash, contract string) (uint64, []byte, error) {
	body, err := json.Marshal(rpcRequest{
		JSONRPC: "2.0", ID: 1,
		Method: "eth_getTransactionReceipt",
		Params: []any{txHash},
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
		if strings.EqualFold(l.Address, contract) {
			data, err := hex.DecodeString(strings.TrimPrefix(l.Data, "0x"))
			if err != nil {
				return 0, nil, err
			}
			return height, data, nil
		}
	}
	return 0, nil, fmt.Errorf("no log from %s in transaction", contract)
}
