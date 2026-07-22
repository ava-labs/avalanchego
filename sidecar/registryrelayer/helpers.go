package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/netip"
	"os"
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
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/utils/compression"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"

	p2ppb "github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
)

var warpPrecompileAddr = common.HexToAddress("0x0200000000000000000000000000000000000005")

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

type teleporterFeeInfo struct {
	FeeTokenAddress common.Address
	Amount          *big.Int
}

type teleporterICMMessage struct {
	Message            teleporterMessageV2
	SourceNetworkID    uint32
	SourceBlockchainID [32]byte
	Attestation        []byte
}

// eventDecoderABI decodes the non-indexed data of the Teleporter's
// SendCrossChainMessage event: (TeleporterMessageV2 message, TeleporterFeeInfo feeInfo).
// A function with the tuples as OUTPUTS lets us use ABI.UnpackIntoInterface.
var eventDecoderABI = func() abi.ABI {
	const j = `[{"type":"function","name":"d","inputs":[],"outputs":[{"name":"m","type":"tuple","components":[` +
		`{"name":"messageNonce","type":"uint256"},` +
		`{"name":"originSenderAddress","type":"address"},` +
		`{"name":"originTeleporterAddress","type":"address"},` +
		`{"name":"destinationBlockchainID","type":"bytes32"},` +
		`{"name":"destinationAddress","type":"address"},` +
		`{"name":"requiredGasLimit","type":"uint256"},` +
		`{"name":"allowedRelayerAddresses","type":"address[]"},` +
		`{"name":"receipts","type":"tuple[]","components":[{"name":"receivedMessageNonce","type":"uint256"},{"name":"relayerRewardAddress","type":"address"}]},` +
		`{"name":"message","type":"bytes"}]},` +
		`{"name":"f","type":"tuple","components":[{"name":"feeTokenAddress","type":"address"},{"name":"amount","type":"uint256"}]}]}]`
	parsed, err := abi.JSON(strings.NewReader(j))
	if err != nil {
		log.Fatalf("build event decoder ABI: %v", err)
	}
	return parsed
}()

// bytesDecoderABI decodes the SendWarpMessage event data: a single `bytes`
// argument holding the unsigned warp message.
var bytesDecoderABI = func() abi.ABI {
	const j = `[{"type":"function","name":"d","inputs":[],"outputs":[{"name":"b","type":"bytes"}]}]`
	parsed, err := abi.JSON(strings.NewReader(j))
	if err != nil {
		log.Fatalf("build bytes decoder ABI: %v", err)
	}
	return parsed
}()

var (
	sendWarpMessageTopic       = crypto.Keccak256Hash([]byte("SendWarpMessage(address,bytes32,bytes)"))
	sendCrossChainMessageTopic = crypto.Keccak256Hash([]byte(
		"SendCrossChainMessage(bytes32,bytes32,(uint256,address,address,bytes32,address,uint256,address[],(uint256,address)[],bytes),(address,uint256))"))
)

// fetchCChainSend reads the C-Chain tx and returns the unsigned warp message
// (from the precompile's SendWarpMessage log) and the TeleporterMessageV2
// struct (from the Teleporter's SendCrossChainMessage log).
func fetchCChainSend(
	ctx context.Context,
	avalancheURI, txHash string,
	teleporter common.Address,
) (*avalancheWarp.UnsignedMessage, *teleporterMessageV2, error) {
	client, err := ethclient.Dial(strings.TrimSuffix(avalancheURI, "/") + "/ext/bc/C/rpc")
	if err != nil {
		return nil, nil, fmt.Errorf("dial C-Chain: %w", err)
	}
	receipt, err := client.TransactionReceipt(ctx, common.HexToHash(txHash))
	if err != nil {
		return nil, nil, fmt.Errorf("receipt: %w", err)
	}
	if receipt.Status != 1 {
		return nil, nil, fmt.Errorf("send tx reverted")
	}

	var unsigned *avalancheWarp.UnsignedMessage
	var msg *teleporterMessageV2
	for _, l := range receipt.Logs {
		switch {
		case l.Address == warpPrecompileAddr && len(l.Topics) > 0 && l.Topics[0] == sendWarpMessageTopic:
			var out struct{ B []byte }
			if err := bytesDecoderABI.UnpackIntoInterface(&out, "d", l.Data); err != nil {
				return nil, nil, fmt.Errorf("unpack SendWarpMessage data: %w", err)
			}
			unsigned, err = avalancheWarp.ParseUnsignedMessage(out.B)
			if err != nil {
				return nil, nil, fmt.Errorf("parse unsigned warp message: %w", err)
			}
		case l.Address == teleporter && len(l.Topics) > 0 && l.Topics[0] == sendCrossChainMessageTopic:
			var out struct {
				M teleporterMessageV2
				F teleporterFeeInfo
			}
			if err := eventDecoderABI.UnpackIntoInterface(&out, "d", l.Data); err != nil {
				return nil, nil, fmt.Errorf("unpack SendCrossChainMessage data: %w", err)
			}
			m := out.M
			msg = &m
		}
	}
	if unsigned == nil {
		return nil, nil, fmt.Errorf("no SendWarpMessage log from the warp precompile in tx")
	}
	if msg == nil {
		return nil, nil, fmt.Errorf("no SendCrossChainMessage log from teleporter %s in tx", teleporter)
	}
	return unsigned, msg, nil
}

// --- ACP-118 signature collection from the primary-network validators ---

func collectSignatures(
	ctx context.Context,
	networkID uint32,
	chainID ids.ID,
	protocolPrefix []byte,
	unsigned *avalancheWarp.UnsignedMessage,
	justification []byte,
	validatorAddrs []string,
) map[ids.NodeID][]byte {
	requestPayload, err := proto.Marshal(&sdk.SignatureRequest{
		Message:       unsigned.Bytes(),
		Justification: justification,
	})
	if err != nil {
		log.Fatalf("marshal SignatureRequest: %v", err)
	}
	prefixed := append(append([]byte{}, protocolPrefix...), requestPayload...)

	out := make(map[ids.NodeID][]byte)
	for _, addr := range validatorAddrs {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		nodeID, sig, err := requestOne(ctx, networkID, chainID, prefixed, addr)
		if err != nil {
			log.Printf("validator at %s: %v", addr, err)
			continue
		}
		out[nodeID] = sig
	}
	return out
}

func requestOne(
	ctx context.Context,
	networkID uint32,
	chainID ids.ID,
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
				resultCh <- result{err: fmt.Errorf("validator refused: %s", appErr.ErrorMessage)}
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
	appRequest, err := mb.AppRequest(chainID, 1, 30*time.Second, prefixedRequest)
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

// --- Delivery to the external chain ---

func deliver(
	ctx context.Context,
	besuRPC, teleporterArtifact, besuKeyHex string,
	teleporter common.Address,
	msg *teleporterMessageV2,
	networkID uint32,
	sourceChainID ids.ID,
	attestation []byte,
) (*types.Receipt, error) {
	teleporterABI, err := loadABI(teleporterArtifact)
	if err != nil {
		return nil, err
	}
	icm := teleporterICMMessage{
		Message:            *msg,
		SourceNetworkID:    networkID,
		SourceBlockchainID: [32]byte(sourceChainID),
		Attestation:        attestation,
	}
	callData, err := teleporterABI.Pack("receiveCrossChainMessage", icm, common.Address{})
	if err != nil {
		return nil, fmt.Errorf("pack receiveCrossChainMessage: %w", err)
	}

	key, err := crypto.HexToECDSA(strings.TrimPrefix(besuKeyHex, "0x"))
	if err != nil {
		return nil, fmt.Errorf("parse besu key: %w", err)
	}
	client, err := ethclient.Dial(besuRPC)
	if err != nil {
		return nil, fmt.Errorf("dial external chain: %w", err)
	}
	chainID, err := client.ChainID(ctx)
	if err != nil {
		return nil, err
	}
	nonce, err := client.PendingNonceAt(ctx, ethAddress(key))
	if err != nil {
		return nil, err
	}
	tx := types.MustSignNewTx(key, types.LatestSignerForChainID(chainID), &types.DynamicFeeTx{
		ChainID: chainID, Nonce: nonce,
		GasTipCap: big.NewInt(1_000_000_000), GasFeeCap: big.NewInt(100_000_000_000),
		Gas: 8_000_000, To: &teleporter, Data: callData,
	})
	if err := client.SendTransaction(ctx, tx); err != nil {
		return nil, fmt.Errorf("send: %w", err)
	}
	for {
		r, err := client.TransactionReceipt(ctx, tx.Hash())
		if err == nil {
			return r, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timed out waiting for receipt %s", tx.Hash())
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func ethAddress(key *ecdsa.PrivateKey) common.Address {
	return crypto.PubkeyToAddress(key.PublicKey)
}

func loadABI(path string) (abi.ABI, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return abi.ABI{}, fmt.Errorf("read %s: %w", path, err)
	}
	var a struct {
		ABI json.RawMessage `json:"abi"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return abi.ABI{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return abi.JSON(strings.NewReader(string(a.ABI)))
}
