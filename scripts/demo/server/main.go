// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// server is a demo HTTP server for CreateL1Tx. It provides:
//   - POST /api/create-l1      — issue CreateL1Tx (SSE stream)
//   - POST /api/start-l1       — restart nodes with track-subnets (SSE stream)
//   - GET  /api/l1s            — list of L1s created this session
//   - GET  /api/blocks         — last N P-Chain blocks with decoded transactions
//   - POST /api/transfer       — send AVAX between pre-funded keys (SSE stream)
//   - GET  /api/xsvm-balance   — balance on an XSVM chain
//   - POST /api/xsvm-transfer  — send tokens on an XSVM chain (SSE stream)
//   - GET  /api/xsvm-blocks    — recent blocks on an XSVM chain
//   - GET  /api/network        — node info
//
// Usage:
//
//	go run ./scripts/demo/server [--network-dir <path>] [--port <port>]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/ids"
	pblock "github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/api"
	xsvmgenesis "github.com/ava-labs/avalanchego/vms/example/xsvm/genesis"
	xsvmtx "github.com/ava-labs/avalanchego/vms/example/xsvm/tx"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	platformapi "github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

// ── Registry ──────────────────────────────────────────────────────────────────

type createdL1 struct {
	TxID                string    `json:"txID"`
	SubnetID            string    `json:"subnetID"`
	BlockchainID        string    `json:"blockchainID"`
	GenesisValidationID string    `json:"genesisValidationID"`
	ConversionID        string    `json:"conversionID"`
	ChainName           string    `json:"chainName"`
	ValidatorNodeID     string    `json:"validatorNodeID"`
	ValidatorWeight     uint64    `json:"validatorWeight"`
	ValidatorBalance    uint64    `json:"validatorBalance"`
	IssueDuration       string    `json:"issueDuration"`
	ConfirmDuration     string    `json:"confirmDuration"`
	CreatedAt           time.Time `json:"createdAt"`
	Checks              []check   `json:"checks"`
	// XSVM chain fields
	GenesisKeyIndex int  `json:"genesisKeyIndex"`
	ChainRunning    bool `json:"chainRunning"`
}

type check struct {
	Label  string `json:"label"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

var (
	mu      sync.Mutex
	created []createdL1
)

// ── main ──────────────────────────────────────────────────────────────────────

var networkDir string

func main() {
	flag.StringVar(&networkDir, "network-dir", filepath.Join(os.Getenv("HOME"), ".tmpnet/networks/latest"), "path to tmpnet network directory")
	port := flag.String("port", "8080", "port to serve on")
	flag.Parse()

	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/api/network", handleNetwork)
	http.HandleFunc("/api/create-l1", handleCreateL1)
	http.HandleFunc("/api/start-l1", handleStartL1)
	http.HandleFunc("/api/l1s", handleL1s)
	http.HandleFunc("/api/blocks", handleBlocks)
	http.HandleFunc("/api/transfer", handleTransfer)
	http.HandleFunc("/api/xsvm-balance", handleXSVMBalance)
	http.HandleFunc("/api/xsvm-transfer", handleXSVMTransfer)
	http.HandleFunc("/api/xsvm-blocks", handleXSVMBlocks)

	addr := ":" + *port
	log.Printf("Demo server at http://localhost%s  (network: %s)", addr, networkDir)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func cors(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
}

// loadNetwork reads the network from disk with a no-op logger so node.Restart works.
func loadNetwork(ctx context.Context) (*tmpnet.Network, error) {
	return tmpnet.ReadNetwork(ctx, logging.NoLog{}, networkDir)
}

func makeWallet(ctx context.Context, network *tmpnet.Network, keyIdx int, nodeIdx int) (*primary.Wallet, error) {
	uri := network.Nodes[nodeIdx].GetAccessibleURI()
	key := network.PreFundedKeys[keyIdx]
	kc := secp256k1fx.NewKeychain(key)
	base, err := primary.MakeWallet(ctx, uri, kc, kc, primary.WalletConfig{})
	if err != nil {
		return nil, err
	}
	return primary.NewWalletWithOptions(base), nil
}

func txTypeName(utx txs.UnsignedTx) string {
	t := reflect.TypeOf(utx)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t.Name()
}

// findL1 looks up a createdL1 by subnet ID string (caller must not hold mu).
func findL1(subnetIDStr string) (createdL1, bool) {
	mu.Lock()
	defer mu.Unlock()
	for _, e := range created {
		if e.SubnetID == subnetIDStr {
			return e, true
		}
	}
	return createdL1{}, false
}

// markChainRunning sets ChainRunning=true for the given subnet ID.
func markChainRunning(subnetIDStr string) {
	mu.Lock()
	defer mu.Unlock()
	for i := range created {
		if created[i].SubnetID == subnetIDStr {
			created[i].ChainRunning = true
			return
		}
	}
}

// ── /api/network ──────────────────────────────────────────────────────────────

func handleNetwork(w http.ResponseWriter, r *http.Request) {
	cors(w)
	w.Header().Set("Content-Type", "application/json")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	network, err := loadNetwork(ctx)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	type nodeInfo struct {
		Index  int    `json:"index"`
		NodeID string `json:"nodeID"`
		URI    string `json:"uri"`
	}
	nodes := make([]nodeInfo, len(network.Nodes))
	for i, n := range network.Nodes {
		nodes[i] = nodeInfo{i, n.NodeID.String(), n.GetAccessibleURI()}
	}
	type keyInfo struct {
		Index   int    `json:"index"`
		Address string `json:"address"`
	}
	keys := make([]keyInfo, len(network.PreFundedKeys))
	for i, k := range network.PreFundedKeys {
		keys[i] = keyInfo{i, k.Address().String()}
	}
	json.NewEncoder(w).Encode(map[string]any{
		"nodeCount": len(nodes),
		"nodes":     nodes,
		"keyCount":  len(keys),
		"keys":      keys,
	})
}

// ── /api/l1s ──────────────────────────────────────────────────────────────────

func handleL1s(w http.ResponseWriter, r *http.Request) {
	cors(w)
	w.Header().Set("Content-Type", "application/json")
	mu.Lock()
	defer mu.Unlock()
	list := created
	if list == nil {
		list = []createdL1{}
	}
	json.NewEncoder(w).Encode(list)
}

// ── /api/blocks ───────────────────────────────────────────────────────────────

type blockInfo struct {
	Height    uint64   `json:"height"`
	BlockID   string   `json:"blockID"`
	Timestamp string   `json:"timestamp"`
	TxCount   int      `json:"txCount"`
	Txs       []txInfo `json:"txs"`
}

type txInfo struct {
	TxID string `json:"txID"`
	Type string `json:"type"`
	Memo string `json:"memo,omitempty"`
}

func handleBlocks(w http.ResponseWriter, r *http.Request) {
	cors(w)
	w.Header().Set("Content-Type", "application/json")
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	network, err := loadNetwork(ctx)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	nodeURI := network.Nodes[0].GetAccessibleURI()
	pClient := platformvm.NewClient(nodeURI)

	height, err := pClient.GetHeight(ctx)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	limit := uint64(15)
	var blocks []blockInfo
	for h := height; h > 0 && uint64(len(blocks)) < limit; h-- {
		rawBytes, err := pClient.GetBlockByHeight(ctx, h)
		if err != nil {
			break
		}
		blk, err := pblock.Parse(pblock.Codec, rawBytes)
		if err != nil {
			continue
		}

		var txInfos []txInfo
		for _, tx := range blk.Txs() {
			memo := ""
			if base, ok := tx.Unsigned.(*txs.BaseTx); ok {
				memo = string(base.Memo)
			}
			txInfos = append(txInfos, txInfo{
				TxID: tx.ID().String(),
				Type: txTypeName(tx.Unsigned),
				Memo: memo,
			})
		}

		blocks = append(blocks, blockInfo{
			Height:  h,
			BlockID: blk.ID().String(),
			Timestamp: func() string {
				if bb, ok := blk.(pblock.BanffBlock); ok {
					return bb.Timestamp().Format(time.RFC3339)
				}
				return "n/a"
			}(),
			TxCount: len(txInfos),
			Txs:     txInfos,
		})
	}

	json.NewEncoder(w).Encode(blocks)
}

// ── /api/transfer (SSE) ───────────────────────────────────────────────────────

func handleTransfer(w http.ResponseWriter, r *http.Request) {
	cors(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}

	sendJSON := func(event string, v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(b))
		flusher.Flush()
	}

	fromIdx, toIdx := 0, 1
	amountAVAX := float64(1)
	fmt.Sscan(r.URL.Query().Get("from"), &fromIdx)
	fmt.Sscan(r.URL.Query().Get("to"), &toIdx)
	fmt.Sscan(r.URL.Query().Get("amount"), &amountAVAX)
	amountNAVAX := uint64(amountAVAX * float64(units.Avax))

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	sendJSON("step", map[string]any{"step": 1, "label": "Loading network", "status": "active"})
	network, err := loadNetwork(ctx)
	if err != nil {
		sendJSON("error", map[string]string{"message": err.Error()})
		return
	}
	if fromIdx >= len(network.PreFundedKeys) || toIdx >= len(network.PreFundedKeys) {
		sendJSON("error", map[string]string{"message": "key index out of range"})
		return
	}
	sendJSON("step", map[string]any{"step": 1, "label": "Loading network", "status": "done",
		"detail": fmt.Sprintf("Sending %.2f AVAX from key[%d] to key[%d]", amountAVAX, fromIdx, toIdx)})

	sendJSON("step", map[string]any{"step": 2, "label": "Building wallet", "status": "active"})
	wallet, err := makeWallet(ctx, network, fromIdx, 0)
	if err != nil {
		sendJSON("error", map[string]string{"message": "wallet error: " + err.Error()})
		return
	}
	pWallet := wallet.P()

	avaxAssetID := pWallet.Builder().Context().AVAXAssetID
	toAddr := network.PreFundedKeys[toIdx].Address()
	sendJSON("step", map[string]any{"step": 2, "label": "Building wallet", "status": "done",
		"detail": fmt.Sprintf("AVAX asset ID: %s", avaxAssetID)})

	sendJSON("step", map[string]any{"step": 3, "label": "Issuing BaseTx (AVAX transfer)", "status": "active"})

	var issuedTxID ids.ID
	var issueDur, confirmDur time.Duration

	walletWithHandlers := primary.NewWalletWithOptions(
		wallet,
		common.WithIssuanceHandler(func(r common.IssuanceReceipt) {
			issuedTxID = r.TxID
			issueDur = r.Duration
		}),
		common.WithConfirmationHandler(func(r common.ConfirmationReceipt) {
			confirmDur = r.ConfirmationDuration
		}),
	)

	tx, err := walletWithHandlers.P().IssueBaseTx(
		[]*avax.TransferableOutput{{
			Asset: avax.Asset{ID: avaxAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: amountNAVAX,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{toAddr},
				},
			},
		}},
		common.WithContext(ctx),
	)
	if err != nil {
		sendJSON("error", map[string]string{"message": "transfer failed: " + err.Error()})
		return
	}

	sendJSON("step", map[string]any{"step": 3, "label": "Issuing BaseTx (AVAX transfer)", "status": "done",
		"detail": fmt.Sprintf("txID: %s  issued in %s", issuedTxID, issueDur)})
	sendJSON("step", map[string]any{"step": 4, "label": "Confirmed by Snowman consensus", "status": "done",
		"detail": fmt.Sprintf("confirmed in %s", confirmDur)})

	sendJSON("result", map[string]any{
		"txID":        tx.ID().String(),
		"from":        network.PreFundedKeys[fromIdx].Address().String(),
		"to":          toAddr.String(),
		"amountNAVAX": amountNAVAX,
		"amountAVAX":  amountAVAX,
		"issueDur":    issueDur.String(),
		"confirmDur":  confirmDur.String(),
	})
	sendJSON("done", struct{}{})
}

// ── /api/create-l1 (SSE) ─────────────────────────────────────────────────────

func handleCreateL1(w http.ResponseWriter, r *http.Request) {
	cors(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	send := func(event, data string) {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
		flusher.Flush()
	}
	sendJSON := func(event string, v any) {
		b, _ := json.Marshal(v)
		send(event, string(b))
	}

	nodeIndex := 0
	chainName := r.URL.Query().Get("chainName")
	if chainName == "" {
		chainName = "DemoL1"
	}
	fmt.Sscan(r.URL.Query().Get("nodeIndex"), &nodeIndex)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	send("step", `{"step":1,"label":"Load network from disk","status":"active"}`)
	network, err := loadNetwork(ctx)
	if err != nil {
		sendJSON("error", map[string]string{"message": err.Error()})
		return
	}
	if nodeIndex >= len(network.Nodes) {
		sendJSON("error", map[string]string{"message": fmt.Sprintf("nodeIndex %d out of range", nodeIndex)})
		return
	}
	if len(network.PreFundedKeys) < 2 {
		sendJSON("error", map[string]string{"message": "network needs at least 2 pre-funded keys"})
		return
	}
	sendJSON("step", map[string]any{"step": 1, "label": "Load network from disk", "status": "done",
		"detail": fmt.Sprintf("%d nodes, %d pre-funded keys", len(network.Nodes), len(network.PreFundedKeys))})

	send("step", `{"step":2,"label":"Fetch validator NodeID + BLS proof-of-possession","status":"active"}`)
	nodeURI := network.Nodes[nodeIndex].GetAccessibleURI()
	infoClient := info.NewClient(nodeURI)
	nodeID, pop, err := infoClient.GetNodeID(ctx)
	if err != nil {
		sendJSON("error", map[string]string{"message": "GetNodeID: " + err.Error()})
		return
	}
	sendJSON("step", map[string]any{"step": 2, "label": "Fetch validator NodeID + BLS proof-of-possession", "status": "done",
		"detail": fmt.Sprintf("NodeID: %s  BLS: %x...", nodeID, pop.PublicKey[:6])})

	send("step", `{"step":3,"label":"Build P-Chain wallet + XSVM genesis","status":"active"}`)
	kc := secp256k1fx.NewKeychain(network.PreFundedKeys[0])
	baseWallet, err := primary.MakeWallet(ctx, nodeURI, kc, kc, primary.WalletConfig{})
	if err != nil {
		sendJSON("error", map[string]string{"message": "MakeWallet: " + err.Error()})
		return
	}

	// Build XSVM genesis: fund pre-funded key[1] with half of max uint64 tokens.
	genesisKeyIndex := 1
	genesisKey := network.PreFundedKeys[genesisKeyIndex]
	genesisBytes, err := xsvmgenesis.Codec.Marshal(xsvmgenesis.CodecVersion, &xsvmgenesis.Genesis{
		Timestamp: time.Now().Unix(),
		Allocations: []xsvmgenesis.Allocation{{
			Address: genesisKey.Address(),
			Balance: math.MaxUint64 / 2,
		}},
	})
	if err != nil {
		sendJSON("error", map[string]string{"message": "genesis marshal: " + err.Error()})
		return
	}

	var issuedTxID, confirmedTxID ids.ID
	var issueDur, confirmDur time.Duration
	wallet := primary.NewWalletWithOptions(baseWallet,
		common.WithIssuanceHandler(func(r common.IssuanceReceipt) {
			issuedTxID = r.TxID
			issueDur = r.Duration
		}),
		common.WithConfirmationHandler(func(r common.ConfirmationReceipt) {
			confirmedTxID = r.TxID
			confirmDur = r.ConfirmationDuration
		}),
	)
	sendJSON("step", map[string]any{"step": 3, "label": "Build P-Chain wallet + XSVM genesis", "status": "done",
		"detail": fmt.Sprintf("genesis key: %s  genesis bytes: %d bytes", genesisKey.Address(), len(genesisBytes))})

	send("step", `{"step":4,"label":"Build + sign CreateL1Tx (XSVM chain)","status":"active"}`)
	createL1Tx, err := wallet.P().IssueCreateL1Tx(
		chainName, constants.XSVMID, nil, genesisBytes, ids.Empty, []byte{},
		[]*txs.CreateL1Validator{{
			NodeID: nodeID.Bytes(), Weight: units.Schmeckle, Balance: units.Avax, Signer: *pop,
		}},
		common.WithContext(ctx),
	)
	if err != nil {
		sendJSON("error", map[string]string{"message": "CreateL1Tx: " + err.Error()})
		return
	}
	sendJSON("step", map[string]any{"step": 4, "label": "Build + sign CreateL1Tx (XSVM chain)", "status": "done",
		"detail": fmt.Sprintf("txID: %s  (%s)", issuedTxID, issueDur)})

	sendJSON("step", map[string]any{"step": 5, "label": "Snowman consensus — tx accepted by all nodes", "status": "done",
		"detail": fmt.Sprintf("confirmed %s in %s", confirmedTxID, confirmDur)})

	send("step", `{"step":6,"label":"Verify committed state via P-Chain API","status":"active"}`)
	subnetID := createL1Tx.ID()
	genesisValidationID := subnetID.Append(0)
	blockchainID := createL1Tx.Unsigned.(*txs.CreateL1Tx).BlockchainID(subnetID)
	pClient := platformvm.NewClient(nodeURI)

	var checks []check
	subnet, err := pClient.GetSubnet(ctx, subnetID)
	checks = append(checks,
		check{"GetSubnet: subnetID == txID", err == nil && subnetID == createL1Tx.ID(), subnetID.String()},
		check{"GetSubnet: IsPermissioned == false (L1)", err == nil && !subnet.IsPermissioned, fmt.Sprintf("%v", subnet.IsPermissioned)},
		check{"GetSubnet: ConversionID is set", err == nil && subnet.ConversionID != ids.Empty, subnet.ConversionID.String()},
		check{"BlockchainID = SHA256(subnetID || 0x00)", blockchainID != ids.Empty && blockchainID != subnetID, blockchainID.String()},
	)

	height, _ := pClient.GetHeight(ctx)
	validators, err := pClient.GetValidatorsAt(ctx, subnetID, platformapi.Height(height))
	checks = append(checks, check{"GetValidatorsAt: initial validator registered", err == nil && len(validators) == 1,
		fmt.Sprintf("%d validator(s) at height %d", len(validators), height)})
	for id, v := range validators {
		checks = append(checks, check{"Validator NodeID matches tx", id == nodeID,
			fmt.Sprintf("%s  weight=%d", id, v.Weight)})
	}

	l1Val, _, err := pClient.GetL1Validator(ctx, genesisValidationID)
	checks = append(checks,
		check{"GetL1Validator: validationID = subnetID.Append(0)", err == nil, genesisValidationID.String()},
		check{"GetL1Validator: balance = 1 AVAX", err == nil && l1Val.Balance == units.Avax, fmt.Sprintf("%d nAVAX", l1Val.Balance)},
	)

	passed := 0
	for _, c := range checks {
		if c.Passed {
			passed++
		}
	}
	sendJSON("step", map[string]any{"step": 6, "label": "Verify committed state via P-Chain API", "status": "done",
		"detail": fmt.Sprintf("%d/%d checks passed", passed, len(checks))})

	entry := createdL1{
		TxID: createL1Tx.ID().String(), SubnetID: subnetID.String(),
		BlockchainID: blockchainID.String(), GenesisValidationID: genesisValidationID.String(),
		ConversionID: subnet.ConversionID.String(), ChainName: chainName,
		ValidatorNodeID: nodeID.String(), ValidatorWeight: units.Schmeckle, ValidatorBalance: units.Avax,
		IssueDuration: issueDur.String(), ConfirmDuration: confirmDur.String(),
		CreatedAt: time.Now(), Checks: checks,
		GenesisKeyIndex: genesisKeyIndex,
	}
	mu.Lock()
	// Replace any existing entry with the same subnetID; otherwise append.
	replaced := false
	for i, e := range created {
		if e.SubnetID == entry.SubnetID {
			created[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		created = append(created, entry)
	}
	mu.Unlock()

	sendJSON("result", map[string]any{
		"txID": createL1Tx.ID().String(), "subnetID": subnetID.String(),
		"blockchainID": blockchainID.String(), "genesisValidationID": genesisValidationID.String(),
		"isPermissioned": subnet.IsPermissioned, "conversionID": subnet.ConversionID.String(),
		"chainName": chainName, "validatorNodeID": nodeID.String(),
		"validatorBalance": l1Val.Balance,
		"issueDuration": issueDur.String(), "confirmDuration": confirmDur.String(),
		"allChecksPassed": passed == len(checks), "checks": checks,
		"genesisKeyIndex": genesisKeyIndex,
	})
	send("done", `{}`)
}

// ── /api/start-l1 (SSE) ──────────────────────────────────────────────────────
// Restarts all nodes with --track-subnets=<subnetID> and waits until the
// XSVM chain reaches Validating status on the first node.

func handleStartL1(w http.ResponseWriter, r *http.Request) {
	cors(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	sendJSON := func(event string, v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(b))
		flusher.Flush()
	}

	subnetIDStr := r.URL.Query().Get("subnetID")
	subnetID, err := ids.FromString(subnetIDStr)
	if err != nil {
		sendJSON("error", map[string]string{"message": "invalid subnetID: " + err.Error()})
		return
	}

	entry, ok := findL1(subnetIDStr)
	if !ok {
		sendJSON("error", map[string]string{"message": "L1 not found in registry — create it first"})
		return
	}
	blockchainID, _ := ids.FromString(entry.BlockchainID)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	sendJSON("step", map[string]any{"step": 1, "label": "Loading network config", "status": "active"})
	network, err := loadNetwork(ctx)
	if err != nil {
		sendJSON("error", map[string]string{"message": err.Error()})
		return
	}
	sendJSON("step", map[string]any{"step": 1, "label": "Loading network config", "status": "done",
		"detail": fmt.Sprintf("%d nodes to restart", len(network.Nodes))})

	// Kill any ghost nodes — those running without a process_context.json.
	// When nodes are started outside of tmpnet (e.g. restart_nodes.go) or
	// their process_context.json is missing, tmpnet's Stop() sees pid=0 and
	// skips the kill, so Start() then races with a still-running instance and
	// fails on the database lock. Resolve the symlink so the pkill pattern
	// matches the flags.json path embedded in each node's argv.
	sendJSON("step", map[string]any{"step": 2, "label": "Stopping any running nodes (clearing stale state)", "status": "active"})
	realNetworkDir, _ := filepath.EvalSymlinks(networkDir)
	exec.Command("pkill", "-9", "-f", realNetworkDir).Run() //nolint: errcheck
	time.Sleep(2 * time.Second)
	// Remove process.json files so getProcess() sees pid=0 and does not
	// mistake a recycled PID for a still-running node on the next Start().
	for _, n := range network.Nodes {
		os.Remove(filepath.Join(n.DataDir, config.DefaultProcessContextFilename)) //nolint: errcheck
	}
	sendJSON("step", map[string]any{"step": 2, "label": "Stopping any running nodes (clearing stale state)", "status": "done",
		"detail": "all nodes stopped"})

	// Clear all in-memory URIs so GetBootstrapIPsAndIDs treats every node as
	// not-yet-started. This lets node[0] start solo (no bootstrap IPs), then
	// node[1] bootstraps from node[0] alone, node[2] from node[0]+node[1],
	// etc. Without this clear, stale URIs from process.json would make every
	// node try to bootstrap from the dead pre-pkill addresses → 0 peers.
	for _, n := range network.Nodes {
		n.URI = ""
	}

	sendJSON("step", map[string]any{"step": 3, "label": fmt.Sprintf("Restarting %d nodes sequentially with track-subnets=%s", len(network.Nodes), subnetID), "status": "active"})

	// Start nodes SEQUENTIALLY so each node's waitForProcessContext updates
	// its staking address before the next node calls composeFlags().
	for i, node := range network.Nodes {
		// Append subnetID to any existing track-subnets value.
		existing := ""
		if v, ok := node.Flags[config.TrackSubnetsKey]; ok {
			existing = fmt.Sprintf("%v", v)
		}
		if existing == "" {
			node.Flags[config.TrackSubnetsKey] = subnetID.String()
		} else if !strings.Contains(existing, subnetID.String()) {
			node.Flags[config.TrackSubnetsKey] = existing + "," + subnetID.String()
		}

		// Remove stale hardcoded bootstrap flags so composeFlags() recomputes
		// them from the current live staking addresses of the other nodes.
		// Without this, SetDefault in composeFlags never fires and nodes start
		// with dead peer addresses, preventing peer discovery.
		delete(node.Flags, config.BootstrapIPsKey)
		delete(node.Flags, config.BootstrapIDsKey)

		sendJSON("progress", map[string]any{
			"msg": fmt.Sprintf("starting node %d/%d: %s", i+1, len(network.Nodes), node.NodeID),
		})
		// Nodes were already killed by pkill above — skip Stop() and go straight
		// to Start(). node.Write() persists updated flags (track-subnets, cleared
		// bootstrap addresses) then node.Start() writes flags.json with freshly
		// computed bootstrap IPs from whatever nodes are already running.
		if err := node.Write(); err != nil {
			sendJSON("error", map[string]string{"message": fmt.Sprintf("node %s write failed: %s", node.NodeID, err)})
			return
		}
		if err := node.Start(ctx); err != nil {
			sendJSON("error", map[string]string{"message": fmt.Sprintf("node %s start failed: %s", node.NodeID, err)})
			return
		}
	}

	sendJSON("step", map[string]any{"step": 3, "label": fmt.Sprintf("Restarting %d nodes sequentially with track-subnets=%s", len(network.Nodes), subnetID), "status": "done",
		"detail": "all nodes restarted successfully"})

	// Use the validator node's URI (nodeIndex from the L1 creation).
	validatorIdx := 0
	for i, n := range network.Nodes {
		if n.NodeID.String() == entry.ValidatorNodeID {
			validatorIdx = i
			break
		}
	}
	nodeURI := network.Nodes[validatorIdx].GetAccessibleURI()
	pClient := platformvm.NewClient(nodeURI)

	sendJSON("step", map[string]any{"step": 4, "label": "Waiting for XSVM chain to reach Validating status", "status": "active",
		"detail": fmt.Sprintf("polling blockchainID: %s", blockchainID)})

	pollCtx, pollCancel := context.WithTimeout(ctx, 3*time.Minute)
	defer pollCancel()
	for {
		select {
		case <-pollCtx.Done():
			sendJSON("error", map[string]string{"message": "timed out waiting for chain to reach Validating status"})
			return
		case <-time.After(3 * time.Second):
		}

		chainStatus, err := pClient.GetBlockchainStatus(pollCtx, blockchainID.String())
		if err != nil {
			continue
		}
		if chainStatus == status.Validating {
			break
		}
		sendJSON("progress", map[string]any{"status": chainStatus.String()})
	}

	markChainRunning(subnetIDStr)

	sendJSON("step", map[string]any{"step": 4, "label": "Waiting for XSVM chain to reach Validating status", "status": "done",
		"detail": "chain is Validating — XSVM L1 is live!"})
	sendJSON("result", map[string]any{
		"subnetID":     subnetIDStr,
		"blockchainID": blockchainID.String(),
		"chainRunning": true,
	})
	sendJSON("done", struct{}{})
}

// ── /api/xsvm-balance ─────────────────────────────────────────────────────────

func handleXSVMBalance(w http.ResponseWriter, r *http.Request) {
	cors(w)
	w.Header().Set("Content-Type", "application/json")
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	subnetIDStr := r.URL.Query().Get("subnetID")
	entry, ok := findL1(subnetIDStr)
	if !ok {
		http.Error(w, "L1 not found", 404)
		return
	}
	blockchainID, _ := ids.FromString(entry.BlockchainID)

	network, err := loadNetwork(ctx)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	keyIdx := entry.GenesisKeyIndex
	if keyIdx >= len(network.PreFundedKeys) {
		keyIdx = 0
	}
	genesisKey := network.PreFundedKeys[keyIdx]
	nodeURI := network.Nodes[0].GetAccessibleURI()

	xsvmClient := api.NewClient(nodeURI, blockchainID.String())
	balance, err := xsvmClient.Balance(ctx, genesisKey.Address(), blockchainID)
	if err != nil {
		http.Error(w, "xsvm balance error: "+err.Error(), 500)
		return
	}
	nonce, err := xsvmClient.Nonce(ctx, genesisKey.Address())
	if err != nil {
		http.Error(w, "xsvm nonce error: "+err.Error(), 500)
		return
	}

	json.NewEncoder(w).Encode(map[string]any{
		"address":      genesisKey.Address().String(),
		"balance":      balance,
		"nonce":        nonce,
		"blockchainID": blockchainID.String(),
	})
}

// ── /api/xsvm-transfer (SSE) ─────────────────────────────────────────────────

func handleXSVMTransfer(w http.ResponseWriter, r *http.Request) {
	cors(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	sendJSON := func(event string, v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(b))
		flusher.Flush()
	}

	subnetIDStr := r.URL.Query().Get("subnetID")
	toKeyIdx := 2
	var amount uint64 = 1000
	fmt.Sscan(r.URL.Query().Get("toKeyIndex"), &toKeyIdx)
	fmt.Sscan(r.URL.Query().Get("amount"), &amount)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	sendJSON("step", map[string]any{"step": 1, "label": "Load network + L1 registry", "status": "active"})
	entry, ok := findL1(subnetIDStr)
	if !ok {
		sendJSON("error", map[string]string{"message": "L1 not found — create it first"})
		return
	}
	if !entry.ChainRunning {
		sendJSON("error", map[string]string{"message": "chain is not running yet — click 'Start L1 Chain' first"})
		return
	}
	blockchainID, _ := ids.FromString(entry.BlockchainID)

	network, err := loadNetwork(ctx)
	if err != nil {
		sendJSON("error", map[string]string{"message": err.Error()})
		return
	}
	if toKeyIdx >= len(network.PreFundedKeys) {
		sendJSON("error", map[string]string{"message": "toKeyIndex out of range"})
		return
	}
	sendJSON("step", map[string]any{"step": 1, "label": "Load network + L1 registry", "status": "done",
		"detail": fmt.Sprintf("chain: %s  amount: %d tokens", entry.ChainName, amount)})

	fromKey := network.PreFundedKeys[entry.GenesisKeyIndex]
	toAddr := network.PreFundedKeys[toKeyIdx].Address()
	nodeURI := network.Nodes[0].GetAccessibleURI()
	xsvmClient := api.NewClient(nodeURI, blockchainID.String())

	sendJSON("step", map[string]any{"step": 2, "label": "Fetch sender nonce", "status": "active"})
	nonce, err := xsvmClient.Nonce(ctx, fromKey.Address())
	if err != nil {
		sendJSON("error", map[string]string{"message": "nonce error: " + err.Error()})
		return
	}
	sendJSON("step", map[string]any{"step": 2, "label": "Fetch sender nonce", "status": "done",
		"detail": fmt.Sprintf("address: %s  nonce: %d", fromKey.Address(), nonce)})

	sendJSON("step", map[string]any{"step": 3, "label": "Sign + issue XSVM Transfer tx", "status": "active"})
	utx := &xsvmtx.Transfer{
		ChainID: blockchainID,
		Nonce:   nonce,
		MaxFee:  0,
		AssetID: blockchainID, // native asset = blockchainID
		Amount:  amount,
		To:      toAddr,
	}
	signedTx, err := xsvmtx.Sign(utx, fromKey)
	if err != nil {
		sendJSON("error", map[string]string{"message": "sign error: " + err.Error()})
		return
	}
	txID, err := xsvmClient.IssueTx(ctx, signedTx)
	if err != nil {
		sendJSON("error", map[string]string{"message": "IssueTx error: " + err.Error()})
		return
	}
	sendJSON("step", map[string]any{"step": 3, "label": "Sign + issue XSVM Transfer tx", "status": "done",
		"detail": fmt.Sprintf("txID: %s", txID)})

	sendJSON("step", map[string]any{"step": 4, "label": "Waiting for tx acceptance", "status": "active"})
	if err := api.AwaitTxAccepted(ctx, xsvmClient, fromKey.Address(), nonce, 500*time.Millisecond); err != nil {
		sendJSON("error", map[string]string{"message": "await error: " + err.Error()})
		return
	}
	sendJSON("step", map[string]any{"step": 4, "label": "Waiting for tx acceptance", "status": "done",
		"detail": "tx accepted by XSVM chain"})

	// Read final balances.
	fromBal, _ := xsvmClient.Balance(ctx, fromKey.Address(), blockchainID)
	toBal, _ := xsvmClient.Balance(ctx, toAddr, blockchainID)

	sendJSON("result", map[string]any{
		"txID":        txID.String(),
		"chainName":   entry.ChainName,
		"blockchainID": blockchainID.String(),
		"from":        fromKey.Address().String(),
		"to":          toAddr.String(),
		"amount":      amount,
		"fromBalance": fromBal,
		"toBalance":   toBal,
	})
	sendJSON("done", struct{}{})
}

// ── /api/xsvm-blocks ─────────────────────────────────────────────────────────

type xsvmBlockInfo struct {
	BlockID   string          `json:"blockID"`
	ParentID  string          `json:"parentID"`
	Timestamp string          `json:"timestamp"`
	TxCount   int             `json:"txCount"`
	Txs       []xsvmTxInfo    `json:"txs"`
}

type xsvmTxInfo struct {
	TxID    string `json:"txID"`
	Type    string `json:"type"`
	From    string `json:"from,omitempty"`
	To      string `json:"to,omitempty"`
	Amount  uint64 `json:"amount,omitempty"`
}

func handleXSVMBlocks(w http.ResponseWriter, r *http.Request) {
	cors(w)
	w.Header().Set("Content-Type", "application/json")
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	subnetIDStr := r.URL.Query().Get("subnetID")
	entry, ok := findL1(subnetIDStr)
	if !ok {
		http.Error(w, "L1 not found", 404)
		return
	}
	if !entry.ChainRunning {
		json.NewEncoder(w).Encode([]xsvmBlockInfo{})
		return
	}
	blockchainID, _ := ids.FromString(entry.BlockchainID)

	network, err := loadNetwork(ctx)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	nodeURI := network.Nodes[0].GetAccessibleURI()
	xsvmClient := api.NewClient(nodeURI, blockchainID.String())

	lastID, lastBlk, err := xsvmClient.LastAccepted(ctx)
	if err != nil {
		http.Error(w, "xsvm lastAccepted error: "+err.Error(), 500)
		return
	}

	var blocks []xsvmBlockInfo
	currentID := lastID
	currentBlk := lastBlk

	for i := 0; i < 10 && currentBlk != nil; i++ {
		var txInfos []xsvmTxInfo
		for _, tx := range currentBlk.Txs {
			info := xsvmTxInfo{Type: reflect.TypeOf(tx.Unsigned).Elem().Name()}
			if txID, err := tx.ID(); err == nil {
				info.TxID = txID.String()
			}
			if transfer, ok := tx.Unsigned.(*xsvmtx.Transfer); ok {
				info.Type = "Transfer"
				info.Amount = transfer.Amount
				info.To = transfer.To.String()
				// recover sender from signature
				if senderID, err := tx.SenderID(); err == nil {
					info.From = senderID.String()
				}
			}
			txInfos = append(txInfos, info)
		}

		blocks = append(blocks, xsvmBlockInfo{
			BlockID:   currentID.String(),
			ParentID:  currentBlk.ParentID.String(),
			Timestamp: time.Unix(currentBlk.Timestamp, 0).Format(time.RFC3339),
			TxCount:   len(currentBlk.Txs),
			Txs:       txInfos,
		})

		// Walk to parent.
		if currentBlk.ParentID == ids.Empty || i >= 9 {
			break
		}
		parentBlk, err := xsvmClient.Block(ctx, currentBlk.ParentID)
		if err != nil {
			break
		}
		currentID = currentBlk.ParentID
		currentBlk = parentBlk
	}

	json.NewEncoder(w).Encode(blocks)
}

// ── / ─────────────────────────────────────────────────────────────────────────

func handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, indexHTML)
}

const indexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>CreateL1Tx Demo</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:'SF Mono','Fira Code',monospace;background:#0d1117;color:#e6edf3;min-height:100vh;padding:32px 20px}
.container{max-width:960px;margin:0 auto}
h1{font-size:1.5rem;color:#58a6ff;margin-bottom:3px}
.subtitle{color:#8b949e;font-size:.82rem;margin-bottom:24px}

/* Tabs */
.tabs{display:flex;gap:0;border-bottom:1px solid #30363d;margin-bottom:24px}
.tab{padding:10px 20px;cursor:pointer;font-size:.85rem;color:#8b949e;border-bottom:2px solid transparent;margin-bottom:-1px;transition:color .15s}
.tab:hover{color:#e6edf3}
.tab.active{color:#58a6ff;border-bottom-color:#58a6ff}
.tab-content{display:none}
.tab-content.active{display:block}

/* Cards */
.card{background:#161b22;border:1px solid #30363d;border-radius:8px;padding:20px;margin-bottom:20px}
.card h2{font-size:.9rem;color:#e6edf3;margin-bottom:14px}
.card.green{border-color:#238636}
.card.red{border-color:#f85149}
.card.blue{border-color:#1f6feb}

/* Diagram */
.diagram{display:flex;gap:14px;align-items:center;background:#161b22;border:1px solid #30363d;border-radius:8px;padding:16px 20px;margin-bottom:20px;flex-wrap:wrap}
.dsid{flex:1;min-width:180px}
.dsid h3{font-size:.68rem;color:#8b949e;text-transform:uppercase;letter-spacing:1px;margin-bottom:7px}
.dsid.before .txb{background:#21262d;border:1px solid #6e4040;border-radius:5px;padding:6px 10px;margin-bottom:4px;font-size:.76rem;color:#f85149}
.dsid.after  .txb{background:#21262d;border:1px solid #1f6feb;border-radius:5px;padding:6px 10px;font-size:.76rem;color:#58a6ff}
.arr{font-size:1.3rem;color:#3fb950;padding:0 6px;align-self:center}

/* Form */
.form-row{display:flex;gap:10px;flex-wrap:wrap;align-items:flex-end}
.fg{display:flex;flex-direction:column;gap:5px;flex:1;min-width:140px}
label{font-size:.7rem;color:#8b949e}
input,select{background:#0d1117;border:1px solid #30363d;border-radius:5px;padding:7px 10px;color:#e6edf3;font-family:inherit;font-size:.82rem;outline:none}
input:focus,select:focus{border-color:#58a6ff}
.btn{background:#238636;border:1px solid #2ea043;border-radius:5px;padding:7px 16px;color:#fff;font-family:inherit;font-size:.85rem;cursor:pointer;white-space:nowrap;transition:background .15s}
.btn:hover{background:#2ea043}
.btn:disabled{background:#21262d;border-color:#30363d;color:#484f58;cursor:not-allowed}
.btn.blue{background:#1f6feb;border-color:#388bfd}
.btn.blue:hover{background:#388bfd}
.btn.orange{background:#9a4a00;border-color:#d29922}
.btn.orange:hover{background:#b85c00}

/* Steps */
.step{display:flex;align-items:flex-start;gap:9px;padding:8px 0;border-bottom:1px solid #21262d;font-size:.8rem}
.step:last-child{border-bottom:none}
.si{width:16px;text-align:center;flex-shrink:0;margin-top:1px}
.sb{flex:1}
.sd{color:#8b949e;font-size:.73rem;margin-top:2px;word-break:break-all}
.step.pending .si,.step.pending .sl{color:#484f58}
.step.active  .si,.step.active  .sl{color:#d29922}
.step.done    .si,.step.done    .sl{color:#3fb950}

/* Checks */
.chk{display:flex;gap:9px;padding:6px 0;border-bottom:1px solid #21262d;font-size:.78rem}
.chk:last-child{border-bottom:none}
.chk-p{color:#3fb950;flex-shrink:0}
.chk-f{color:#f85149;flex-shrink:0}
.chk-d{color:#8b949e;font-size:.71rem;margin-top:2px;word-break:break-all}

/* Result grid */
.rg{display:grid;grid-template-columns:1fr 1fr;gap:9px;margin-bottom:12px}
@media(max-width:600px){.rg{grid-template-columns:1fr}}
.ri{background:#0d1117;border-radius:5px;padding:9px 11px}
.ri.full{grid-column:1/-1}
.rk{font-size:.66rem;color:#8b949e;text-transform:uppercase;letter-spacing:.4px;margin-bottom:3px}
.rv{font-size:.74rem;color:#e6edf3;word-break:break-all}
.rv.blue{color:#58a6ff}
.rv.green{color:#3fb950}
.badge{display:inline-block;padding:2px 7px;border-radius:10px;font-size:.67rem;font-weight:bold}
.badge.l1{background:#1f3d7a;color:#58a6ff}
.badge.running{background:#1a3a1a;color:#3fb950}
.badge.stopped{background:#3a1a1a;color:#f85149}
.timing{color:#8b949e;font-size:.72rem;margin-top:10px}

/* Explorer table */
.block-item{margin-bottom:12px;background:#0d1117;border-radius:6px;padding:10px 14px}
.block-header{display:flex;gap:14px;align-items:center;margin-bottom:6px}
.block-height{color:#58a6ff;font-size:.82rem;font-weight:bold;min-width:60px}
.block-id{color:#8b949e;font-size:.72rem;flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.block-time{color:#484f58;font-size:.7rem}
.tx-row{display:flex;gap:10px;align-items:center;padding:3px 0;border-top:1px solid #21262d;font-size:.75rem}
.tx-type{min-width:120px;color:#e6edf3}
.tx-type.CreateL1Tx{color:#58a6ff}
.tx-type.BaseTx{color:#3fb950}
.tx-type.Transfer{color:#d29922}
.tx-type.AddPermissionlessValidatorTx,.tx-type.AddValidatorTx{color:#d29922}
.tx-id{color:#484f58;font-size:.7rem;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;flex:1}
.empty{color:#484f58;text-align:center;padding:20px;font-size:.83rem}
.refresh-note{color:#484f58;font-size:.7rem;margin-bottom:10px}

/* Transfer result */
.t-res{margin-top:12px;background:#0d1117;border-radius:6px;padding:12px}

.spinner{display:inline-block;animation:spin .8s linear infinite}
@keyframes spin{to{transform:rotate(360deg)}}
.hidden{display:none}

/* Balance display */
.balance-box{background:#0d1117;border-radius:6px;padding:12px 16px;margin-bottom:14px}
.bal-label{font-size:.68rem;color:#8b949e;text-transform:uppercase;letter-spacing:.4px;margin-bottom:4px}
.bal-value{font-size:1.2rem;color:#3fb950;font-weight:bold}
.bal-addr{font-size:.68rem;color:#484f58;margin-top:3px;word-break:break-all}
</style>
</head>
<body>
<div class="container">
<h1>CreateL1Tx Demo</h1>
<p class="subtitle">P-Chain + XSVM L1 demo running against a local 5-node tmpnet</p>

<div class="tabs">
  <div class="tab active" onclick="switchTab('create',event)">Create L1</div>
  <div class="tab" onclick="switchTab('l1chain',event)">L1 Chain</div>
  <div class="tab" onclick="switchTab('explorer',event)">P-Chain Explorer</div>
  <div class="tab" onclick="switchTab('history',event)">Created L1s</div>
</div>

<!-- ═══ CREATE L1 ══════════════════════════════════════════════════════════ -->
<div id="tab-create" class="tab-content active">
  <div class="diagram">
    <div class="dsid before">
      <h3>Before (3 txs)</h3>
      <div class="txb">1. CreateSubnetTx → subnetID</div>
      <div class="txb">2. CreateChainTx &nbsp;→ chainID</div>
      <div class="txb">3. ConvertSubnetToL1Tx → validators</div>
    </div>
    <div class="arr">→</div>
    <div class="dsid after">
      <h3>After — ACP-191 (1 tx)</h3>
      <div class="txb">CreateL1Tx → subnetID + chainID + validators</div>
    </div>
  </div>

  <div class="card">
    <h2>Issue a CreateL1Tx (XSVM chain)</h2>
    <div class="form-row">
      <div class="fg"><label>Chain Name</label><input id="chainName" value="DemoL1"></div>
      <div class="fg"><label>Genesis Validator</label><select id="nodeIndex"><option>Loading...</option></select></div>
      <button class="btn" id="createBtn" onclick="issueCreate()">Create L1</button>
    </div>
  </div>

  <div class="card red hidden" id="createError" style="color:#f85149;font-size:.83rem"></div>

  <div class="card hidden" id="stepsCard">
    <h2>Transaction Flow</h2>
    <div id="stepsList"></div>
  </div>

  <div class="card hidden" id="checksCard">
    <h2>Verification Checks</h2>
    <div id="checksList"></div>
  </div>

  <div class="card green hidden" id="resultCard">
    <h2>✓ L1 Created</h2>
    <div id="resultContent"></div>
    <div id="startChainArea" class="hidden" style="margin-top:16px;padding-top:16px;border-top:1px solid #30363d">
      <p style="color:#8b949e;font-size:.78rem;margin-bottom:10px">The L1 exists on the P-Chain. Now restart nodes to track the subnet and start the XSVM chain:</p>
      <button class="btn orange" id="startChainBtn" onclick="startChain()">▶ Start L1 Chain</button>
    </div>
  </div>

  <div class="card hidden" id="startStepsCard">
    <h2>Starting XSVM Chain</h2>
    <div id="startStepsList"></div>
  </div>
</div>

<!-- ═══ L1 CHAIN ══════════════════════════════════════════════════════════ -->
<div id="tab-l1chain" class="tab-content">
  <div class="card">
    <h2>L1 Chain Explorer &amp; Transfer</h2>
    <div class="form-row" style="margin-bottom:14px">
      <div class="fg"><label>Select L1</label><select id="l1Selector" onchange="onL1Select()"><option value="">— no L1s yet —</option></select></div>
      <button class="btn" style="padding:7px 12px;font-size:.75rem" onclick="refreshL1Chain()">↻ Refresh</button>
    </div>
    <div id="l1Status" class="hidden"></div>
  </div>

  <div id="l1BalanceCard" class="card hidden">
    <h2>Genesis Account Balance</h2>
    <div id="l1BalanceContent"></div>
  </div>

  <div id="l1TransferCard" class="card hidden">
    <h2>Send XSVM Tokens</h2>
    <div class="form-row">
      <div class="fg"><label>To (key index)</label><select id="xsvmTo"></select></div>
      <div class="fg"><label>Amount (tokens)</label><input id="xsvmAmount" type="number" value="1000" min="1"></div>
      <button class="btn blue" id="xsvmBtn" onclick="issueXSVMTransfer()">Send Tokens</button>
    </div>
    <div class="card red hidden" id="xsvmError" style="color:#f85149;font-size:.83rem;margin-top:10px"></div>
    <div class="card hidden" id="xsvmStepsCard" style="margin-top:10px">
      <div id="xsvmStepsList"></div>
    </div>
    <div class="card green hidden" id="xsvmResultCard" style="margin-top:10px">
      <h2>✓ Transfer Confirmed on L1</h2>
      <div id="xsvmResultContent"></div>
    </div>
  </div>

  <div id="l1BlocksCard" class="card hidden">
    <h2>XSVM Chain Blocks <button class="btn" style="float:right;padding:4px 12px;font-size:.75rem" onclick="loadXSVMBlocks()">↻ Refresh</button></h2>
    <div id="l1BlocksContent"><div class="empty">Select an L1 to view blocks</div></div>
  </div>
</div>

<!-- ═══ EXPLORER ══════════════════════════════════════════════════════════ -->
<div id="tab-explorer" class="tab-content">
  <div class="card">
    <h2>Recent P-Chain Blocks <button class="btn" style="float:right;padding:4px 12px;font-size:.75rem" onclick="loadBlocks()">↻ Refresh</button></h2>
    <div class="refresh-note">Auto-refreshes every 5s. CreateL1Tx and BaseTx will appear here.</div>
    <div id="blocksContent"><div class="empty">Loading blocks...</div></div>
  </div>
</div>

<!-- ═══ HISTORY ═══════════════════════════════════════════════════════════ -->
<div id="tab-history" class="tab-content">
  <div class="card">
    <h2>Created L1s this session</h2>
    <div id="historyContent"><div class="empty">No L1s created yet</div></div>
  </div>
</div>

</div><!-- /container -->

<script>
// ── state ─────────────────────────────────────────────────────────────────
let _lastSubnetID = null;
let _lastResult = null;
let _networkKeys = [];

// ── Tab switching ─────────────────────────────────────────────────────────
function switchTab(name, evt) {
  document.querySelectorAll('.tab-content').forEach(el => el.classList.remove('active'));
  document.querySelectorAll('.tab').forEach(el => el.classList.remove('active'));
  document.getElementById('tab-' + name).classList.add('active');
  if (evt && evt.target) evt.target.classList.add('active');
  if (name === 'explorer') loadBlocks();
  if (name === 'history') loadHistory();
  if (name === 'l1chain') { populateL1Selector(); loadXSVMBlocks(); }
}

// ── Bootstrap: load network info ─────────────────────────────────────────
function loadNetworkInfo() {
  fetch('/api/network').then(r => r.json()).then(d => {
    const nodeSel = document.getElementById('nodeIndex');
    nodeSel.innerHTML = (d.nodes||[]).map(n =>
      '<option value="'+n.index+'">Node '+n.index+' ('+n.nodeID.slice(8,22)+'...)</option>'
    ).join('');
    _networkKeys = d.keys || [];
    ['txFrom','txTo','xsvmTo'].forEach((id,i) => {
      const el = document.getElementById(id);
      if (!el) return;
      el.innerHTML = _networkKeys.map(k =>
        '<option value="'+k.index+'">Key '+k.index+' ('+k.address.slice(0,12)+'...)</option>'
      ).join('');
      if (id === 'txTo' && _networkKeys.length > 1) el.selectedIndex = 1;
      if (id === 'xsvmTo' && _networkKeys.length > 2) el.selectedIndex = 2;
    });
  }).catch(()=>{});
}
loadNetworkInfo();

// ── P-Chain Explorer ──────────────────────────────────────────────────────
function loadBlocks() {
  fetch('/api/blocks').then(r => r.json()).then(renderBlocks).catch(()=>{});
}
setInterval(loadBlocks, 5000);
loadBlocks();

function renderBlocks(blocks) {
  const el = document.getElementById('blocksContent');
  if (!blocks || !blocks.length) { el.innerHTML='<div class="empty">No blocks yet</div>'; return; }
  el.innerHTML = blocks.map(b => {
    const txRows = (b.txs||[]).map(tx =>
      '<div class="tx-row"><span class="tx-type '+tx.type+'">'+tx.type+'</span><span class="tx-id">'+tx.txID+'</span></div>'
    ).join('') || '<div class="tx-row" style="color:#484f58">no transactions</div>';
    return '<div class="block-item">' +
      '<div class="block-header">' +
        '<span class="block-height">#'+b.height+'</span>' +
        '<span class="block-id">'+b.blockID+'</span>' +
        '<span class="block-time">'+b.timestamp+'</span>' +
      '</div>' + txRows + '</div>';
  }).join('');
}

// ── History ───────────────────────────────────────────────────────────────
function loadHistory() {
  fetch('/api/l1s').then(r => r.json()).then(renderHistory).catch(()=>{});
}

function renderHistory(list) {
  const el = document.getElementById('historyContent');
  if (!list||!list.length) { el.innerHTML='<div class="empty">No L1s created yet</div>'; return; }
  el.innerHTML = list.slice().reverse().map(l => {
    const p = (l.checks||[]).filter(c=>c.passed).length;
    const t = (l.checks||[]).length;
    const badge = l.chainRunning
      ? '<span class="badge running">● XSVM Running</span>'
      : '<span class="badge stopped">○ Chain not started</span>';
    return '<div class="block-item" style="margin-bottom:14px">' +
      '<div class="block-header">' +
        '<span class="block-height" style="color:#58a6ff;min-width:auto;margin-right:10px">'+l.chainName+'</span>' +
        badge +
        '<span class="block-time" style="margin-left:auto">'+new Date(l.createdAt).toLocaleTimeString()+'</span>' +
      '</div>' +
      '<div class="tx-row"><span style="color:#8b949e;min-width:120px">SubnetID</span><span class="tx-id">'+l.subnetID+'</span></div>' +
      '<div class="tx-row"><span style="color:#8b949e;min-width:120px">BlockchainID</span><span class="tx-id">'+l.blockchainID+'</span></div>' +
      '<div class="tx-row"><span style="color:#8b949e;min-width:120px">Validator</span><span class="tx-id">'+l.validatorNodeID+'</span></div>' +
      '<div class="tx-row"><span style="color:#8b949e;min-width:120px">Confirm time</span><span class="tx-id">'+l.confirmDuration+'</span><span style="color:#3fb950;margin-left:12px">'+p+'/'+t+' checks ✓</span></div>' +
    '</div>';
  }).join('');
}

// ── SSE step helpers ──────────────────────────────────────────────────────
const CREATE_STEPS = [
  {id:1,label:'Load network from disk'},
  {id:2,label:'Fetch validator NodeID + BLS proof-of-possession'},
  {id:3,label:'Build P-Chain wallet + XSVM genesis'},
  {id:4,label:'Build + sign CreateL1Tx (XSVM chain)'},
  {id:5,label:'Snowman consensus — tx accepted by all nodes'},
  {id:6,label:'Verify committed state via P-Chain API'},
];
const TX_STEPS = [
  {id:1,label:'Loading network'},
  {id:2,label:'Building wallet'},
  {id:3,label:'Issuing BaseTx (AVAX transfer)'},
  {id:4,label:'Confirmed by Snowman consensus'},
];
const START_STEPS = [
  {id:1,label:'Loading network config'},
  {id:2,label:'Stopping any running nodes'},
  {id:3,label:'Restarting nodes with track-subnets flag'},
  {id:4,label:'Waiting for XSVM chain to reach Validating status'},
];
const XSVM_STEPS = [
  {id:1,label:'Load network + L1 registry'},
  {id:2,label:'Fetch sender nonce'},
  {id:3,label:'Sign + issue XSVM Transfer tx'},
  {id:4,label:'Waiting for tx acceptance'},
];

function initSteps(listId, steps) {
  document.getElementById(listId).innerHTML = steps.map(s =>
    '<div class="step pending" id="'+listId+'-step-'+s.id+'">'+
      '<div class="si">○</div>'+
      '<div class="sb"><div class="sl">'+s.label+'</div>'+
      '<div class="sd" id="'+listId+'-sd-'+s.id+'"></div></div>'+
    '</div>'
  ).join('');
}

function setStep(listId, id, status, label, detail) {
  const el = document.getElementById(listId+'-step-'+id);
  if (!el) return;
  el.className = 'step ' + status;
  const icons = {pending:'○', active:'<span class="spinner">⟳</span>', done:'✓'};
  el.querySelector('.si').innerHTML = icons[status]||'○';
  if (label) el.querySelector('.sl').textContent = label;
  const sd = document.getElementById(listId+'-sd-'+id);
  if (detail && sd) sd.textContent = detail;
}

// ── Create L1 ─────────────────────────────────────────────────────────────
function issueCreate() {
  const chainName = document.getElementById('chainName').value||'DemoL1';
  const nodeIndex = document.getElementById('nodeIndex').value;
  const btn = document.getElementById('createBtn');
  btn.disabled = true;
  ['createError','resultCard','checksCard','startStepsCard'].forEach(id => document.getElementById(id).classList.add('hidden'));
  const sc = document.getElementById('stepsCard');
  sc.classList.remove('hidden');
  initSteps('stepsList', CREATE_STEPS);

  const es = new EventSource('/api/create-l1?chainName='+encodeURIComponent(chainName)+'&nodeIndex='+nodeIndex);
  es.addEventListener('step', e => { const d=JSON.parse(e.data); setStep('stepsList',d.step,d.status,d.label,d.detail||''); });
  es.addEventListener('result', e => {
    const d = JSON.parse(e.data);
    _lastResult = d;
    _lastSubnetID = d.subnetID;
    if (d.checks) renderChecks(d.checks);
    renderCreateResult(d);
    loadHistory();
    loadBlocks();
    populateL1Selector();
  });
  es.addEventListener('error', e => {
    try { document.getElementById('createError').textContent='✗ '+JSON.parse(e.data).message; } catch{}
    document.getElementById('createError').classList.remove('hidden');
    es.close(); btn.disabled=false;
  });
  es.addEventListener('done', () => { es.close(); btn.disabled=false; });
  es.onerror = () => { es.close(); btn.disabled=false; };
}

function renderChecks(checks) {
  const card = document.getElementById('checksCard');
  card.classList.remove('hidden');
  document.getElementById('checksList').innerHTML = checks.map(c =>
    '<div class="chk"><div class="'+(c.passed?'chk-p':'chk-f')+'">'+(c.passed?'✓':'✗')+'</div>'+
    '<div><div>'+c.label+'</div><div class="chk-d">'+c.detail+'</div></div></div>'
  ).join('');
}

function renderCreateResult(d) {
  document.getElementById('resultCard').classList.remove('hidden');
  document.getElementById('resultContent').innerHTML =
    '<div class="rg">'+
      ri('TxID = SubnetID', d.subnetID, 'blue')+
      ri('BlockchainID (SHA256(subnetID||0x00))', d.blockchainID, 'blue')+
      ri('Genesis ValidationID (subnetID.Append(0))', d.genesisValidationID, '')+
      ri('Conversion ID', d.conversionID, '')+
      ri('Subnet Type', '<span class="badge l1">L1 — permissionless</span>', '')+
      ri('Chain VM', 'XSVM', 'green')+
      ri('Validator Balance', (d.validatorBalance/1e9).toFixed(2)+' AVAX', 'green')+
      ri('Chain Name', d.chainName, '')+
    '</div>'+
    '<div class="timing">Issued in '+d.issueDuration+' · Confirmed in '+d.confirmDuration+'</div>';
  document.getElementById('startChainArea').classList.remove('hidden');
  // Reset button so it doesn't carry over "✓ Chain Running" state from a previous run.
  const btn = document.getElementById('startChainBtn');
  btn.disabled = false;
  btn.textContent = '▶ Start L1 Chain';
  btn.style.background=''; btn.style.borderColor=''; btn.style.color='';
}

// ── Start L1 Chain ────────────────────────────────────────────────────────
function startChain() {
  if (!_lastSubnetID) { alert('No L1 created yet'); return; }
  const btn = document.getElementById('startChainBtn');
  btn.disabled = true;
  btn.textContent = '⟳ Starting...';

  const sc = document.getElementById('startStepsCard');
  sc.classList.remove('hidden');
  initSteps('startStepsList', START_STEPS);

  const es = new EventSource('/api/start-l1?subnetID='+encodeURIComponent(_lastSubnetID));
  es.addEventListener('step', e => { const d=JSON.parse(e.data); setStep('startStepsList',d.step,d.status,d.label,d.detail||''); });
  es.addEventListener('progress', e => {
    const d=JSON.parse(e.data);
    if (d.msg) {
      const sd = document.getElementById('startStepsList-sd-3');
      if (sd) sd.textContent = d.msg;
    } else if (d.status) {
      const sd = document.getElementById('startStepsList-sd-4');
      if (sd) sd.textContent = 'chain status: '+d.status+' — waiting...';
    }
  });
  es.addEventListener('result', e => {
    const d=JSON.parse(e.data);
    btn.textContent = '✓ Chain Running';
    btn.style.background='#1a3a1a'; btn.style.borderColor='#3fb950'; btn.style.color='#3fb950';
    loadHistory();
    populateL1Selector();
    // Auto-switch to L1 Chain tab
    switchTab('l1chain', null);
    selectL1(d.subnetID);
  });
  es.addEventListener('error', e => {
    try { alert('Start L1 error: '+JSON.parse(e.data).message); } catch{}
    btn.disabled=false; btn.textContent='▶ Start L1 Chain';
    es.close();
  });
  es.addEventListener('done', () => { es.close(); });
  es.onerror = () => { es.close(); };
}

// ── L1 Chain tab ─────────────────────────────────────────────────────────
let _l1List = [];

function populateL1Selector() {
  fetch('/api/l1s').then(r => r.json()).then(list => {
    _l1List = list || [];
    const sel = document.getElementById('l1Selector');
    if (!_l1List.length) {
      sel.innerHTML = '<option value="">— no L1s yet —</option>';
      return;
    }
    sel.innerHTML = _l1List.map(l =>
      '<option value="'+l.subnetID+'">'+(l.chainRunning ? '● ' : '○ ')+l.chainName+' ('+l.subnetID.slice(0,12)+'...)</option>'
    ).join('');
    // If we just started a chain, select it
    if (_lastSubnetID) {
      sel.value = _lastSubnetID;
      onL1Select();
    }
  }).catch(()=>{});
}

function selectL1(subnetID) {
  const sel = document.getElementById('l1Selector');
  sel.value = subnetID;
  onL1Select();
}

function onL1Select() {
  const subnetID = document.getElementById('l1Selector').value;
  if (!subnetID) return;
  const entry = _l1List.find(l => l.subnetID === subnetID);
  if (!entry) return;

  const statusEl = document.getElementById('l1Status');
  statusEl.classList.remove('hidden');

  if (!entry.chainRunning) {
    statusEl.innerHTML =
      '<div style="color:#d29922;font-size:.83rem;padding:8px 0">'+
      '⚠ Chain not started. Go to Create L1 tab and click "Start L1 Chain" for <strong>'+entry.chainName+'</strong>.'+
      '</div>';
    ['l1BalanceCard','l1TransferCard','l1BlocksCard'].forEach(id => document.getElementById(id).classList.add('hidden'));
    return;
  }

  statusEl.innerHTML =
    '<div style="color:#3fb950;font-size:.83rem;padding:8px 0">'+
    '● XSVM chain <strong>'+entry.chainName+'</strong> is live — blockchainID: '+entry.blockchainID+
    '</div>';

  ['l1BalanceCard','l1TransferCard','l1BlocksCard'].forEach(id => document.getElementById(id).classList.remove('hidden'));
  loadXSVMBalance(subnetID);
  loadXSVMBlocks(subnetID);
}

function refreshL1Chain() {
  populateL1Selector();
  const subnetID = document.getElementById('l1Selector').value;
  if (subnetID) {
    loadXSVMBalance(subnetID);
    loadXSVMBlocks(subnetID);
  }
}

function loadXSVMBalance(subnetID) {
  subnetID = subnetID || document.getElementById('l1Selector').value;
  if (!subnetID) return;
  fetch('/api/xsvm-balance?subnetID='+encodeURIComponent(subnetID))
    .then(r => r.json())
    .then(d => {
      document.getElementById('l1BalanceContent').innerHTML =
        '<div class="balance-box">'+
          '<div class="bal-label">Native token balance</div>'+
          '<div class="bal-value">'+d.balance.toLocaleString()+' tokens</div>'+
          '<div class="bal-addr">Address: '+d.address+'  ·  Nonce: '+d.nonce+'</div>'+
        '</div>'+
        '<p style="color:#8b949e;font-size:.73rem">Genesis key funded with half of uint64 max. Asset ID = BlockchainID.</p>';
    })
    .catch(e => {
      document.getElementById('l1BalanceContent').innerHTML =
        '<div style="color:#f85149;font-size:.8rem">Balance unavailable: '+e.message+'</div>';
    });
}

function loadXSVMBlocks(subnetID) {
  subnetID = subnetID || document.getElementById('l1Selector').value;
  const el = document.getElementById('l1BlocksContent');
  if (!subnetID) { el.innerHTML = '<div class="empty">Select an L1 to view blocks</div>'; return; }
  fetch('/api/xsvm-blocks?subnetID='+encodeURIComponent(subnetID))
    .then(r => r.json())
    .then(blocks => {
      if (!blocks || !blocks.length) {
        el.innerHTML = '<div class="empty">No blocks yet (chain just started)</div>';
        return;
      }
      el.innerHTML = blocks.map((b,i) => {
        const txRows = (b.txs||[]).map(tx => {
          let detail = '';
          if (tx.type === 'Transfer') detail = tx.amount+' tokens → '+tx.to;
          return '<div class="tx-row">'+
            '<span class="tx-type Transfer">'+tx.type+'</span>'+
            '<span style="color:#e6edf3;font-size:.72rem;min-width:180px">'+detail+'</span>'+
            '<span class="tx-id">'+tx.txID+'</span>'+
          '</div>';
        }).join('') || '<div class="tx-row" style="color:#484f58">no transactions</div>';
        return '<div class="block-item">'+
          '<div class="block-header">'+
            '<span class="block-height">'+(i===0?'latest':'parent')+'</span>'+
            '<span class="block-id">'+b.blockID+'</span>'+
            '<span class="block-time">'+b.timestamp+'</span>'+
          '</div>'+txRows+
        '</div>';
      }).join('');
    })
    .catch(()=>{ el.innerHTML='<div class="empty">Block fetch error</div>'; });
}

// ── XSVM Transfer ─────────────────────────────────────────────────────────
function issueXSVMTransfer() {
  const subnetID = document.getElementById('l1Selector').value;
  if (!subnetID) { alert('Select an L1 first'); return; }
  const toKey = document.getElementById('xsvmTo').value;
  const amount = document.getElementById('xsvmAmount').value || '1000';
  const btn = document.getElementById('xsvmBtn');
  btn.disabled = true;
  ['xsvmError','xsvmResultCard'].forEach(id => document.getElementById(id).classList.add('hidden'));
  const sc = document.getElementById('xsvmStepsCard');
  sc.classList.remove('hidden');
  initSteps('xsvmStepsList', XSVM_STEPS);

  const url = '/api/xsvm-transfer?subnetID='+encodeURIComponent(subnetID)+'&toKeyIndex='+toKey+'&amount='+amount;
  const es = new EventSource(url);
  es.addEventListener('step', e => { const d=JSON.parse(e.data); setStep('xsvmStepsList',d.step,d.status,d.label,d.detail||''); });
  es.addEventListener('result', e => {
    const d=JSON.parse(e.data);
    document.getElementById('xsvmResultCard').classList.remove('hidden');
    document.getElementById('xsvmResultContent').innerHTML =
      '<div class="rg">'+
        ri('TxID', d.txID, 'blue')+
        ri('Chain', d.chainName+' (XSVM)', 'green')+
        ri('Amount', d.amount.toLocaleString()+' tokens', 'green')+
        ri('From balance after', d.fromBalance.toLocaleString()+' tokens', '')+
        ri('To balance after', d.toBalance.toLocaleString()+' tokens', 'green')+
        ri('To address', d.to, '')+
      '</div>';
    loadXSVMBalance(subnetID);
    loadXSVMBlocks(subnetID);
  });
  es.addEventListener('error', e => {
    try { document.getElementById('xsvmError').textContent='✗ '+JSON.parse(e.data).message; } catch{}
    document.getElementById('xsvmError').classList.remove('hidden');
    es.close(); btn.disabled=false;
  });
  es.addEventListener('done', () => { es.close(); btn.disabled=false; });
  es.onerror = () => { es.close(); btn.disabled=false; };
}

// ── P-Chain Transfer ──────────────────────────────────────────────────────
function issueTransfer() {
  const from   = document.getElementById('txFrom').value;
  const to     = document.getElementById('txTo').value;
  const amount = document.getElementById('txAmount').value||'1';
  const btn    = document.getElementById('txBtn');
  btn.disabled = true;
  ['txError','txResultCard'].forEach(id => document.getElementById(id).classList.add('hidden'));
  const sc = document.getElementById('txStepsCard');
  sc.classList.remove('hidden');
  initSteps('txStepsList', TX_STEPS);

  const url = '/api/transfer?from='+from+'&to='+to+'&amount='+amount;
  const es = new EventSource(url);
  es.addEventListener('step', e => { const d=JSON.parse(e.data); setStep('txStepsList',d.step,d.status,d.label,d.detail||''); });
  es.addEventListener('result', e => {
    const d = JSON.parse(e.data);
    document.getElementById('txResultCard').classList.remove('hidden');
    document.getElementById('txResultContent').innerHTML =
      '<div class="rg">'+
        ri('TxID', d.txID, 'blue')+
        ri('Amount', d.amountAVAX+' AVAX ('+d.amountNAVAX+' nAVAX)', 'green')+
        ri('From', d.from, '')+
        ri('To', d.to, '')+
      '</div>'+
      '<div class="timing">Issued in '+d.issueDur+' · Confirmed in '+d.confirmDur+'</div>';
    loadBlocks();
  });
  es.addEventListener('error', e => {
    try { document.getElementById('txError').textContent='✗ '+JSON.parse(e.data).message; } catch{}
    document.getElementById('txError').classList.remove('hidden');
    es.close(); btn.disabled=false;
  });
  es.addEventListener('done', () => { es.close(); btn.disabled=false; });
  es.onerror = () => { es.close(); btn.disabled=false; };
}

function ri(k,v,cls){
  return '<div class="ri"><div class="rk">'+k+'</div><div class="rv'+(cls?' '+cls:'')+'" >'+v+'</div></div>';
}
</script>
</body>
</html>`
