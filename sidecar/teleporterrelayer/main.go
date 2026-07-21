// Command teleporterrelayer is the Phase C step-1b relayer: it turns a real
// TeleporterOutbox event on the external chain into a message delivered by the
// LIVE attestor committee through stock TeleporterMessengerV2 + WarpAdapter.
//
// Flow: read the outbox event (log data = abi.encode(TeleporterMessageV2)) ->
// build the warp UnsignedMessage (AddressedCall{sourceAddress=WarpAdapter,
// payload=that abi.encode}) as the Virtual L1 -> request ACP-118 signatures
// from each attestor at the Teleporter handler ID, with the 60-byte
// justification {txHash, outbox, height} -> verify each vs the canonical
// committee set, aggregate to quorum -> deliver via stock
// receiveCrossChainMessage. The relayer holds no keys and cannot forge.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/oracle/teleporter"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	platformapi "github.com/ava-labs/avalanchego/vms/platformvm/api"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	warppayload "github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

var warpPrecompileAddr = common.HexToAddress("0x0200000000000000000000000000000000000005")

func main() {
	avalancheURI := flag.String("avalanche-uri", "http://127.0.0.1:59501", "Avalanche node API URI")
	subnetIDStr := flag.String("subnet", "", "gateway subnet ID (required)")
	chainIDStr := flag.String("gateway-chain", "", "gateway (Virtual L1) blockchain ID (required)")
	attestorList := flag.String("attestors", "", "comma-separated attestor staking addresses (required)")
	besuRPC := flag.String("besu-rpc", "http://127.0.0.1:9545", "external EVM chain JSON-RPC endpoint")
	txHashHex := flag.String("tx", "", "TeleporterOutbox event tx hash on the external chain (required)")
	outboxStr := flag.String("outbox", "", "TeleporterOutbox contract address (required)")
	warpAdapterStr := flag.String("warp-adapter", "", "destination WarpAdapter address (AddressedCall sourceAddress) (required)")
	teleporterStr := flag.String("teleporter", "", "destination TeleporterMessengerV2 address (required)")
	teleporterArtifact := flag.String("teleporter-abi", "", "path to TeleporterMessengerV2.json for the ABI (required)")
	ethKeyStr := flag.String("eth-key", "", "funded destination-chain key, PrivateKey-... CB58 (required)")
	flag.Parse()

	for name, v := range map[string]string{
		"subnet": *subnetIDStr, "gateway-chain": *chainIDStr, "attestors": *attestorList, "tx": *txHashHex,
		"outbox": *outboxStr, "warp-adapter": *warpAdapterStr, "teleporter": *teleporterStr,
		"teleporter-abi": *teleporterArtifact, "eth-key": *ethKeyStr,
	} {
		if v == "" {
			log.Fatalf("--%s is required", name)
		}
	}
	subnetID := mustID(*subnetIDStr, "subnet")
	gatewayChainID := mustID(*chainIDStr, "gateway-chain")
	txHash := common.HexToHash(*txHashHex)
	outbox := common.HexToAddress(*outboxStr)
	warpAdapter := common.HexToAddress(*warpAdapterStr)
	teleporterAddr := common.HexToAddress(*teleporterStr)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// ---- 1. Read the outbox event: log data = abi.encode(TeleporterMessageV2) ----
	height, teleporterBytes, err := fetchOutboxLog(ctx, *besuRPC, *txHashHex, outbox)
	if err != nil {
		log.Fatalf("read outbox event: %v", err)
	}
	log.Printf("outbox event: block %d, %d bytes of abi.encode(TeleporterMessageV2)", height, len(teleporterBytes))

	// ---- 2. Build the warp UnsignedMessage as the Virtual L1 ----
	infoClient := info.NewClient(*avalancheURI)
	networkID, err := infoClient.GetNetworkID(ctx)
	if err != nil {
		log.Fatalf("network ID: %v", err)
	}
	addressedCall, err := warppayload.NewAddressedCall(warpAdapter.Bytes(), teleporterBytes)
	if err != nil {
		log.Fatalf("addressed call: %v", err)
	}
	unsigned, err := avalancheWarp.NewUnsignedMessage(networkID, gatewayChainID, addressedCall.Bytes())
	if err != nil {
		log.Fatalf("unsigned message: %v", err)
	}

	// ---- 3. Canonical committee set ----
	pClient := platformvm.NewClient(*avalancheURI)
	pHeight, err := pClient.GetHeight(ctx)
	if err != nil {
		log.Fatalf("P-Chain height: %v", err)
	}
	vdrMap, err := pClient.GetValidatorsAt(ctx, subnetID, platformapi.Height(pHeight))
	if err != nil {
		log.Fatalf("validators: %v", err)
	}
	warpSet, err := validators.FlattenValidatorSet(vdrMap)
	if err != nil {
		log.Fatalf("flatten set: %v", err)
	}
	log.Printf("committee: %d validators, total weight %d", len(warpSet.Validators), warpSet.TotalWeight)

	// ---- 4. Request Teleporter-format signatures from each attestor ----
	justification := teleporter.Justification{
		TxHash:      [32]byte(txHash),
		Outbox:      outbox,
		BlockHeight: height,
	}.Encode()
	sigs := collectSignatures(ctx, networkID, gatewayChainID, unsigned, justification, strings.Split(*attestorList, ","))

	// ---- 5. Verify + aggregate to quorum ----
	signerBits := set.NewBits()
	var validSigs []*bls.Signature
	var signedWeight uint64
	for nodeID, sigBytes := range sigs {
		sig, err := bls.SignatureFromBytes(sigBytes)
		if err != nil {
			log.Printf("attestor %s: bad signature: %v", nodeID, err)
			continue
		}
		idx := -1
		for i, vdr := range warpSet.Validators {
			for _, vid := range vdr.NodeIDs {
				if vid == nodeID {
					idx = i
					break
				}
			}
			if idx >= 0 {
				break
			}
		}
		if idx < 0 || !bls.Verify(warpSet.Validators[idx].PublicKey, sig, unsigned.Bytes()) {
			log.Printf("attestor %s: not in set or signature invalid — ignoring", nodeID)
			continue
		}
		if signerBits.Contains(idx) {
			continue
		}
		signerBits.Add(idx)
		validSigs = append(validSigs, sig)
		signedWeight += warpSet.Validators[idx].Weight
		log.Printf("attestor %s: verified (index %d)", nodeID, idx)
	}
	if signedWeight*100 < warpSet.TotalWeight*67 {
		log.Fatalf("QUORUM NOT REACHED: %d/%d weight (%d%%)", signedWeight, warpSet.TotalWeight, signedWeight*100/warpSet.TotalWeight)
	}
	agg, err := bls.AggregateSignatures(validSigs)
	if err != nil {
		log.Fatalf("aggregate: %v", err)
	}
	bitSig := &avalancheWarp.BitSetSignature{Signers: signerBits.Bytes()}
	copy(bitSig.Signature[:], bls.SignatureToBytes(agg))
	signedMsg, err := avalancheWarp.NewMessage(unsigned, bitSig)
	if err != nil {
		log.Fatalf("signed message: %v", err)
	}
	log.Printf("quorum reached: %d/%d attestors, %d%% weight", len(validSigs), len(warpSet.Validators), signedWeight*100/warpSet.TotalWeight)

	// ---- 6. Deliver via stock receiveCrossChainMessage ----
	if err := deliver(ctx, *avalancheURI, *teleporterArtifact, *ethKeyStr, teleporterAddr, teleporterBytes, networkID, gatewayChainID, signedMsg); err != nil {
		log.Fatalf("delivery failed: %v", err)
	}
	fmt.Printf("\nDELIVERED: a TeleporterOutbox event on the external chain, signed by the\n")
	fmt.Printf("live attestor committee, accepted by stock TeleporterMessengerV2 + WarpAdapter\n")
	fmt.Printf("as a message from Virtual L1 %s\n", gatewayChainID)
}

func mustID(s, name string) ids.ID {
	id, err := ids.FromString(s)
	if err != nil {
		log.Fatalf("parse %s: %v", name, err)
	}
	return id
}
