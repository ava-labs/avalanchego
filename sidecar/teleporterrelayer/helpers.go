package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

	"github.com/ava-labs/libevm/accounts/abi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethclient"

	"github.com/ava-labs/avalanchego/ids"
	p2pmessage "github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/utils/compression"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"

	p2ppb "github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
)

// --- Teleporter ABI types (mirror the Solidity struct field order) ---

type teleporterReceipt struct {
	ReceivedMessageNonce *big.Int
	RelayerRewardAddress common.Address
}

type teleporterMessageV2 struct {
	MessageNonce            *big.Int
	OriginSenderAddress     common.Address
	OriginTeleporterAddress common.Address
	DestinationBlockchainID [32]byte
	DestinationAddress      common.Address
	RequiredGasLimit        *big.Int
	AllowedRelayerAddresses []common.Address
	Receipts                []teleporterReceipt
	Message                 []byte
}

type teleporterICMMessage struct {
	Message            teleporterMessageV2
	SourceNetworkID    uint32
	SourceBlockchainID [32]byte
	Attestation        []byte
}

// decoderABI decodes a bare abi.encode(TeleporterMessageV2) into the Go struct.
// A function with the tuple as its single OUTPUT lets us use ABI.UnpackIntoInterface
// (which abi.Arguments lacks in this libevm version).
var decoderABI = func() abi.ABI {
	const j = `[{"type":"function","name":"d","inputs":[],"outputs":[{"name":"m","type":"tuple","components":[` +
		`{"name":"messageNonce","type":"uint256"},` +
		`{"name":"originSenderAddress","type":"address"},` +
		`{"name":"originTeleporterAddress","type":"address"},` +
		`{"name":"destinationBlockchainID","type":"bytes32"},` +
		`{"name":"destinationAddress","type":"address"},` +
		`{"name":"requiredGasLimit","type":"uint256"},` +
		`{"name":"allowedRelayerAddresses","type":"address[]"},` +
		`{"name":"receipts","type":"tuple[]","components":[{"name":"receivedMessageNonce","type":"uint256"},{"name":"relayerRewardAddress","type":"address"}]},` +
		`{"name":"message","type":"bytes"}]}]}]`
	parsed, err := abi.JSON(strings.NewReader(j))
	if err != nil {
		log.Fatalf("build decoder ABI: %v", err)
	}
	return parsed
}()

func teleporterMessageArgs() abi.Arguments {
	t, err := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "messageNonce", Type: "uint256"},
		{Name: "originSenderAddress", Type: "address"},
		{Name: "originTeleporterAddress", Type: "address"},
		{Name: "destinationBlockchainID", Type: "bytes32"},
		{Name: "destinationAddress", Type: "address"},
		{Name: "requiredGasLimit", Type: "uint256"},
		{Name: "allowedRelayerAddresses", Type: "address[]"},
		{Name: "receipts", Type: "tuple[]", Components: []abi.ArgumentMarshaling{
			{Name: "receivedMessageNonce", Type: "uint256"},
			{Name: "relayerRewardAddress", Type: "address"},
		}},
		{Name: "message", Type: "bytes"},
	})
	if err != nil {
		log.Fatalf("teleporter abi type: %v", err)
	}
	return abi.Arguments{{Type: t}}
}

// --- Signature collection at the Teleporter handler ID ---

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
	prefixed := p2p.PrefixMessage(p2p.ProtocolPrefix(p2p.TeleporterSignatureRequestHandlerID), requestPayload)

	out := make(map[ids.NodeID][]byte)
	for _, addr := range attestorAddrs {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		nodeID, sig, err := requestOne(ctx, networkID, gatewayChainID, prefixed, addr)
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

	mb, err := p2pmessage.NewCreator(prometheus.NewRegistry(), compression.TypeZstd, time.Hour)
	if err != nil {
		return ids.EmptyNodeID, nil, err
	}
	// requestID pinned to 1: fresh peer per request (the odd-requestID fix).
	appRequest, err := mb.AppRequest(gatewayChainID, 1, 30*time.Second, prefixedRequest)
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

// --- External-chain event read ---

func fetchOutboxLog(ctx context.Context, rpcURL, txHash string, outbox common.Address) (uint64, []byte, error) {
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "eth_getTransactionReceipt", "params": []any{txHash},
	})
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
	raw, _ := io.ReadAll(resp.Body)
	var envelope struct {
		Result *struct {
			BlockNumber string `json:"blockNumber"`
			Logs        []struct {
				Address string `json:"address"`
				Data    string `json:"data"`
			} `json:"logs"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
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
		if strings.EqualFold(l.Address, outbox.Hex()) {
			return height, common.FromHex(l.Data), nil
		}
	}
	return 0, nil, fmt.Errorf("no log from outbox %s in tx", outbox)
}

// --- Delivery via stock receiveCrossChainMessage ---

func deliver(
	ctx context.Context,
	uri, teleporterArtifactPath, ethKeyStr string,
	teleporterAddr common.Address,
	teleporterBytes []byte,
	networkID uint32,
	gatewayChainID ids.ID,
	signedMsg *avalancheWarp.Message,
) error {
	teleporterABI := loadABI(teleporterArtifactPath)

	// Decode the Teleporter message from the outbox log data to rebuild the
	// receiveCrossChainMessage calldata. abi.Arguments has no UnpackIntoInterface,
	// so use a tiny decoder ABI whose single output is the TeleporterMessageV2 tuple.
	var wrap struct{ M teleporterMessageV2 }
	if err := decoderABI.UnpackIntoInterface(&wrap, "d", teleporterBytes); err != nil {
		return fmt.Errorf("decode TeleporterMessageV2: %w", err)
	}
	msg := wrap.M

	uint32T, _ := abi.NewType("uint32", "", nil)
	attestation, err := abi.Arguments{{Type: uint32T}}.Pack(uint32(0))
	if err != nil {
		return err
	}
	icm := teleporterICMMessage{
		Message:            msg,
		SourceNetworkID:    networkID,
		SourceBlockchainID: [32]byte(gatewayChainID),
		Attestation:        attestation,
	}
	callData, err := teleporterABI.Pack("receiveCrossChainMessage", icm, common.Address{})
	if err != nil {
		return fmt.Errorf("pack receiveCrossChainMessage: %w", err)
	}

	var fundedKey secp256k1.PrivateKey
	if err := fundedKey.UnmarshalText([]byte(ethKeyStr)); err != nil {
		return fmt.Errorf("parse eth key: %w", err)
	}
	ethKey := fundedKey.ToECDSA()
	client, err := ethclient.Dial(strings.TrimSuffix(uri, "/") + "/ext/bc/C/rpc")
	if err != nil {
		return err
	}
	chainID, err := client.ChainID(ctx)
	if err != nil {
		return err
	}
	sender := crypto.PubkeyToAddress(ethKey.PublicKey)
	nonce, err := client.PendingNonceAt(ctx, sender)
	if err != nil {
		return err
	}

	predicate := append(bytes.Clone(signedMsg.Bytes()), 0xff)
	if rem := len(predicate) % 32; rem != 0 {
		predicate = append(predicate, make([]byte, 32-rem)...)
	}
	storageKeys := make([]common.Hash, 0, len(predicate)/32)
	for i := 0; i < len(predicate); i += 32 {
		storageKeys = append(storageKeys, common.BytesToHash(predicate[i:i+32]))
	}

	tx := types.MustSignNewTx(ethKey, types.LatestSignerForChainID(chainID), &types.DynamicFeeTx{
		ChainID: chainID, Nonce: nonce, GasTipCap: big.NewInt(1_000_000_000), GasFeeCap: big.NewInt(100_000_000_000),
		Gas: 6_000_000, To: &teleporterAddr, Data: callData,
		AccessList: types.AccessList{{Address: warpPrecompileAddr, StorageKeys: storageKeys}},
	})
	if err := client.SendTransaction(ctx, tx); err != nil {
		return fmt.Errorf("send receive tx: %w", err)
	}
	receipt, err := waitReceipt(ctx, client, tx.Hash())
	if err != nil {
		return err
	}
	if receipt.Status != 1 {
		return fmt.Errorf("receiveCrossChainMessage reverted (tx %s)", tx.Hash())
	}
	log.Printf("delivered in C-Chain block %d (tx %s)", receipt.BlockNumber, tx.Hash())
	return nil
}

func loadABI(path string) abi.ABI {
	raw, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("read ABI %s: %v", path, err)
	}
	var a struct {
		ABI json.RawMessage `json:"abi"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		log.Fatalf("parse artifact %s: %v", path, err)
	}
	parsed, err := abi.JSON(strings.NewReader(string(a.ABI)))
	if err != nil {
		log.Fatalf("parse ABI %s: %v", path, err)
	}
	return parsed
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
		case <-time.After(400 * time.Millisecond):
		}
	}
}
