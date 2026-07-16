// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Command gatewayrelayer is the attestor-gateway relayer driver: the untrusted
// transport that turns an event on the external chain into a delivered,
// natively-verified message on an Avalanche chain.
//
// Flow: fetch the event from the external EVM chain -> construct the
// OracleMessage a verifier expects -> connect to each attestor as a network
// peer and request a signature over ACP-118 at the oracle handler ID -> verify
// each returned signature individually -> aggregate to a quorum BitSetSignature
// -> deliver to the destination chain as a transaction predicate.
//
// The relayer holds no signing keys for the Virtual L1. It can censor
// (liveness) but cannot forge (safety): every signature comes from an attestor
// that independently verified the event through its own sidecar.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethclient"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	p2pmessage "github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/oracle"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/compression"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	platformapi "github.com/ava-labs/avalanchego/vms/platformvm/api"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	warppayload "github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"

	p2ppb "github.com/ava-labs/avalanchego/proto/pb/p2p"
)

const quorumNumerator = 67 // percent of total stake weight required

var warpPrecompileAddr = common.HexToAddress("0x0200000000000000000000000000000000000005")

func main() {
	avalancheURI := flag.String("avalanche-uri", "http://127.0.0.1:59501", "Avalanche node API URI (P-Chain queries + C-Chain delivery)")
	subnetIDStr := flag.String("subnet", "", "gateway subnet ID (required)")
	chainIDStr := flag.String("gateway-chain", "", "gateway (Virtual L1) blockchain ID (required)")
	attestorList := flag.String("attestors", "", "comma-separated attestor staking addresses, e.g. 127.0.0.1:9661,127.0.0.1:9663 (required)")
	besuRPC := flag.String("besu-rpc", "http://127.0.0.1:9545", "external EVM chain JSON-RPC endpoint")
	txHashHex := flag.String("tx", "", "transaction hash of the event-emitting tx on the external chain (required)")
	contract := flag.String("contract", "", "event-emitting contract address on the external chain (required)")
	proverArtifact := flag.String("prover-artifact", "", "forge artifact JSON for WarpProver (required)")
	ethKeyStr := flag.String("eth-key", "", "funded destination-chain key, PrivateKey-... CB58 (required)")
	tamper := flag.Bool("tamper", false, "corrupt the payload before requesting signatures; attestors must refuse and quorum must fail")
	flag.Parse()

	for name, v := range map[string]string{
		"subnet": *subnetIDStr, "gateway-chain": *chainIDStr, "attestors": *attestorList,
		"tx": *txHashHex, "contract": *contract, "prover-artifact": *proverArtifact, "eth-key": *ethKeyStr,
	} {
		if v == "" {
			log.Fatalf("--%s is required", name)
		}
	}
	subnetID, err := ids.FromString(*subnetIDStr)
	if err != nil {
		log.Fatalf("parse subnet ID: %v", err)
	}
	gatewayChainID, err := ids.FromString(*chainIDStr)
	if err != nil {
		log.Fatalf("parse gateway chain ID: %v", err)
	}
	txHash, err := hex.DecodeString(strings.TrimPrefix(*txHashHex, "0x"))
	if err != nil || len(txHash) != 32 {
		log.Fatalf("--tx must be a 32-byte hex hash")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// ---- 1. Fetch the source event ----
	height, payload, err := fetchLog(ctx, *besuRPC, *txHashHex, *contract)
	if err != nil {
		log.Fatalf("fetch source event: %v", err)
	}
	log.Printf("source event: block %d, %d payload bytes from %s", height, len(payload), *contract)
	if *tamper {
		payload = bytes.Clone(payload)
		payload[len(payload)-1] ^= 0xff
		log.Printf("TAMPER: corrupted final payload byte — attestors must refuse")
	}

	// ---- 2. Construct the message to be signed as the Virtual L1 ----
	infoClient := info.NewClient(*avalancheURI)
	networkID, err := infoClient.GetNetworkID(ctx)
	if err != nil {
		log.Fatalf("get network ID: %v", err)
	}
	oracleMsg, err := oracle.NewOracleMessage(oracle.SourceTypeEVM, *contract, common.Address{}, height, 1, payload)
	if err != nil {
		log.Fatalf("build OracleMessage: %v", err)
	}
	addressedCall, err := warppayload.NewAddressedCall(nil, oracleMsg.Bytes())
	if err != nil {
		log.Fatalf("build AddressedCall: %v", err)
	}
	unsigned, err := avalancheWarp.NewUnsignedMessage(networkID, gatewayChainID, addressedCall.Bytes())
	if err != nil {
		log.Fatalf("build unsigned warp message: %v", err)
	}

	// ---- 3. Canonical validator set (the committee, in verification order) ----
	pClient := platformvm.NewClient(*avalancheURI)
	pHeight, err := pClient.GetHeight(ctx)
	if err != nil {
		log.Fatalf("get P-Chain height: %v", err)
	}
	vdrMap, err := pClient.GetValidatorsAt(ctx, subnetID, platformapi.Height(pHeight))
	if err != nil {
		log.Fatalf("get validators: %v", err)
	}
	warpSet, err := validators.FlattenValidatorSet(vdrMap)
	if err != nil {
		log.Fatalf("flatten validator set: %v", err)
	}
	log.Printf("committee: %d validators, total weight %d, quorum >= %d%%",
		len(warpSet.Validators), warpSet.TotalWeight, quorumNumerator)

	// ---- 4. Request signatures from each attestor over ACP-118 ----
	sigs := collectSignatures(ctx, networkID, gatewayChainID, unsigned, txHash, strings.Split(*attestorList, ","))

	// ---- 5. Verify each signature and build the quorum aggregate ----
	signerBits := set.NewBits()
	var validSigs []*bls.Signature
	var signedWeight uint64
	for nodeID, sigBytes := range sigs {
		sig, err := bls.SignatureFromBytes(sigBytes)
		if err != nil {
			log.Printf("attestor %s: invalid signature bytes: %v", nodeID, err)
			continue
		}
		idx := -1
		for i, vdr := range warpSet.Validators {
			for _, vdrNodeID := range vdr.NodeIDs {
				if vdrNodeID == nodeID {
					idx = i
					break
				}
			}
			if idx >= 0 {
				break
			}
		}
		if idx < 0 {
			log.Printf("attestor %s: not in the registered committee — ignoring", nodeID)
			continue
		}
		if !bls.Verify(warpSet.Validators[idx].PublicKey, sig, unsigned.Bytes()) {
			log.Printf("attestor %s: signature does not verify — ignoring", nodeID)
			continue
		}
		if signerBits.Contains(idx) {
			continue
		}
		signerBits.Add(idx)
		validSigs = append(validSigs, sig)
		signedWeight += warpSet.Validators[idx].Weight
		log.Printf("attestor %s: signature verified (canonical index %d)", nodeID, idx)
	}

	if signedWeight*100 < warpSet.TotalWeight*quorumNumerator {
		log.Fatalf("QUORUM NOT REACHED: signed weight %d of %d (%d%% < %d%%) — message cannot be delivered",
			signedWeight, warpSet.TotalWeight, signedWeight*100/warpSet.TotalWeight, quorumNumerator)
	}
	agg, err := bls.AggregateSignatures(validSigs)
	if err != nil {
		log.Fatalf("aggregate signatures: %v", err)
	}
	bitSig := &avalancheWarp.BitSetSignature{Signers: signerBits.Bytes()}
	copy(bitSig.Signature[:], bls.SignatureToBytes(agg))
	signedMsg, err := avalancheWarp.NewMessage(unsigned, bitSig)
	if err != nil {
		log.Fatalf("assemble signed message: %v", err)
	}
	log.Printf("quorum reached: %d/%d attestors, weight %d/%d (%d%%)",
		len(validSigs), len(warpSet.Validators), signedWeight, warpSet.TotalWeight,
		signedWeight*100/warpSet.TotalWeight)

	// ---- 6. Deliver to the destination chain ----
	if err := deliver(ctx, *avalancheURI, *proverArtifact, *ethKeyStr, signedMsg); err != nil {
		log.Fatalf("delivery failed: %v", err)
	}
	fmt.Printf("\nDELIVERED: event from the external chain, verified by %d independent attestors,\n", len(validSigs))
	fmt.Printf("accepted by the destination chain's stock warp verification as a message from\n")
	fmt.Printf("Virtual L1 %s\n", gatewayChainID)
}

// collectSignatures connects to each attestor as a network peer, sends an
// ACP-118 SignatureRequest at the oracle handler ID, and returns the raw
// signature bytes per responding attestor NodeID. Per-attestor failures are
// logged and skipped — the caller enforces quorum.
func collectSignatures(
	ctx context.Context,
	networkID uint32,
	gatewayChainID ids.ID,
	unsigned *avalancheWarp.UnsignedMessage,
	justification []byte,
	attestorAddrs []string,
) map[ids.NodeID][]byte {
	requestPayload, err := proto.Marshal(&sdk.SignatureRequest{
		Message:       unsigned.Bytes(),
		Justification: justification,
	})
	if err != nil {
		log.Fatalf("marshal SignatureRequest: %v", err)
	}
	prefixed := p2p.PrefixMessage(p2p.ProtocolPrefix(p2p.OracleSignatureRequestHandlerID), requestPayload)

	// Request sequentially: each attestor is a separate peer handshake, and a
	// relayer paces its requests rather than flooding the committee at once.
	// (Concurrent handshakes proved flaky; a production relayer would use a
	// small bounded worker pool — sequential is ample for committee-sized fan-out.)
	out := make(map[ids.NodeID][]byte)
	for i, addr := range attestorAddrs {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		// Each request uses a fresh peer, so a constant requestID is safe and
		// avoids an observed quirk where the handler only answered odd IDs.
		_ = i
		nodeID, sig, err := requestOne(ctx, networkID, gatewayChainID, prefixed, addr, 1)
		if err != nil {
			log.Printf("attestor at %s: %v", addr, err)
			continue
		}
		out[nodeID] = sig
	}
	return out
}

func requestOne(
	ctx context.Context,
	networkID uint32,
	gatewayChainID ids.ID,
	prefixedRequest []byte,
	addr string,
	requestID uint32,
) (ids.NodeID, []byte, error) {
	addrPort, err := netip.ParseAddrPort(addr)
	if err != nil {
		return ids.EmptyNodeID, nil, fmt.Errorf("bad address: %w", err)
	}

	type result struct {
		sig []byte
		err error
	}
	resultCh := make(chan result, 1)

	p, err := peer.StartTestPeer(ctx, addrPort, networkID,
		router.InboundHandlerFunc(func(_ context.Context, msg *p2pmessage.InboundMessage) {
			switch msg.Op {
			case p2pmessage.AppResponseOp:
				resp, ok := msg.Message.(*p2ppb.AppResponse)
				if !ok {
					return
				}
				var sigResp sdk.SignatureResponse
				if err := proto.Unmarshal(resp.AppBytes, &sigResp); err != nil {
					resultCh <- result{err: fmt.Errorf("bad SignatureResponse: %w", err)}
					return
				}
				resultCh <- result{sig: sigResp.Signature}
			case p2pmessage.AppErrorOp:
				appErr, ok := msg.Message.(*p2ppb.AppError)
				if !ok {
					return
				}
				resultCh <- result{err: fmt.Errorf("attestor refused: %s", appErr.ErrorMessage)}
			}
		}),
	)
	if err != nil {
		return ids.EmptyNodeID, nil, fmt.Errorf("peer handshake failed: %w", err)
	}
	defer func() {
		p.StartClose()
		_ = p.AwaitClosed(context.Background())
	}()

	messageBuilder, err := p2pmessage.NewCreator(prometheus.NewRegistry(), compression.TypeZstd, time.Hour)
	if err != nil {
		return ids.EmptyNodeID, nil, err
	}
	appRequest, err := messageBuilder.AppRequest(gatewayChainID, requestID, 30*time.Second, prefixedRequest)
	if err != nil {
		return ids.EmptyNodeID, nil, err
	}
	if !p.Send(ctx, appRequest) {
		return ids.EmptyNodeID, nil, fmt.Errorf("send failed")
	}

	select {
	case r := <-resultCh:
		return p.ID(), r.sig, r.err
	case <-time.After(20 * time.Second):
		return p.ID(), nil, fmt.Errorf("timed out waiting for signature")
	case <-ctx.Done():
		return p.ID(), nil, ctx.Err()
	}
}

// deliver deploys the WarpProver on the destination C-Chain and submits the
// signed message as a transaction predicate — the same delivery mechanics
// proven in Phase A.
func deliver(ctx context.Context, uri, proverArtifactPath, ethKeyStr string, signedMsg *avalancheWarp.Message) error {
	var fundedKey secp256k1.PrivateKey
	if err := fundedKey.UnmarshalText([]byte(ethKeyStr)); err != nil {
		return fmt.Errorf("parse eth key: %w", err)
	}
	ethKey := fundedKey.ToECDSA()

	client, err := ethclient.Dial(strings.TrimSuffix(uri, "/") + "/ext/bc/C/rpc")
	if err != nil {
		return fmt.Errorf("dial C-chain: %w", err)
	}
	chainID, err := client.ChainID(ctx)
	if err != nil {
		return err
	}
	sender := crypto.PubkeyToAddress(ethKey.PublicKey)

	raw, err := os.ReadFile(proverArtifactPath)
	if err != nil {
		return err
	}
	var artifact struct {
		Bytecode struct {
			Object string `json:"object"`
		} `json:"bytecode"`
	}
	if err := json.Unmarshal(raw, &artifact); err != nil {
		return err
	}

	nonce, err := client.PendingNonceAt(ctx, sender)
	if err != nil {
		return err
	}
	gasTip := big.NewInt(1_000_000_000)
	gasFee := big.NewInt(100_000_000_000)
	signer := types.LatestSignerForChainID(chainID)

	deployTx := types.MustSignNewTx(ethKey, signer, &types.DynamicFeeTx{
		ChainID: chainID, Nonce: nonce, GasTipCap: gasTip, GasFeeCap: gasFee,
		Gas: 1_500_000, Data: common.FromHex(artifact.Bytecode.Object),
	})
	if err := client.SendTransaction(ctx, deployTx); err != nil {
		return fmt.Errorf("deploy prover: %w", err)
	}
	deployReceipt, err := waitReceipt(ctx, client, deployTx.Hash())
	if err != nil || deployReceipt.Status != 1 {
		return fmt.Errorf("prover deploy failed: %v", err)
	}
	prover := deployReceipt.ContractAddress

	predicate := append(signedMsg.Bytes(), 0xff)
	if rem := len(predicate) % 32; rem != 0 {
		predicate = append(predicate, make([]byte, 32-rem)...)
	}
	storageKeys := make([]common.Hash, 0, len(predicate)/32)
	for i := 0; i < len(predicate); i += 32 {
		storageKeys = append(storageKeys, common.BytesToHash(predicate[i:i+32]))
	}
	callData := append(crypto.Keccak256([]byte("prove(uint32)"))[:4], make([]byte, 32)...)

	proveTx := types.MustSignNewTx(ethKey, signer, &types.DynamicFeeTx{
		ChainID: chainID, Nonce: nonce + 1, GasTipCap: gasTip, GasFeeCap: gasFee,
		Gas: 5_000_000, To: &prover, Data: callData,
		AccessList: types.AccessList{{Address: warpPrecompileAddr, StorageKeys: storageKeys}},
	})
	if err := client.SendTransaction(ctx, proveTx); err != nil {
		return fmt.Errorf("send prove tx: %w", err)
	}
	receipt, err := waitReceipt(ctx, client, proveTx.Hash())
	if err != nil {
		return err
	}
	if receipt.Status != 1 {
		return fmt.Errorf("prove tx reverted — destination did not verify the message (tx %s)", proveTx.Hash())
	}
	log.Printf("delivered in destination block %d (tx %s, prover %s)", receipt.BlockNumber, proveTx.Hash(), prover)
	return nil
}

func waitReceipt(ctx context.Context, client *ethclient.Client, h common.Hash) (*types.Receipt, error) {
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

func fetchLog(ctx context.Context, rpcURL, txHash, contract string) (uint64, []byte, error) {
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
