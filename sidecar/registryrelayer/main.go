// Command registryrelayer delivers an Avalanche C-Chain Teleporter message to
// an external EVM chain, completing the outbound direction of the gateway.
//
// Flow: read the C-Chain tx's SendWarpMessage log (the unsigned warp message
// the registry adapter submitted to the precompile) and the Teleporter's
// SendCrossChainMessage event (the TeleporterMessageV2 struct) -> request
// ACP-118 signatures over the warp message from the primary-network
// validators (the C-Chain's signers) -> verify each against the canonical
// primary set and aggregate to quorum -> deliver to the same-address
// TeleporterMessengerV2 on the external chain, whose SubsetUpdater registry
// verifies the BLS aggregate on-chain (EIP-2537) against the registered
// primary validator set.
//
// The relayer holds no signing keys for the message itself — like the inbound
// relayers, it can censor but not forge.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	platformapi "github.com/ava-labs/avalanchego/vms/platformvm/api"

	"github.com/ava-labs/libevm/common"
)

func main() {
	avalancheURI := flag.String("avalanche-uri", "http://127.0.0.1:59501", "Avalanche node API URI")
	besuRPC := flag.String("besu-rpc", "http://127.0.0.1:9545", "external EVM chain JSON-RPC endpoint")
	txHashHex := flag.String("tx", "", "C-Chain tx hash of the Teleporter send (required)")
	teleporterStr := flag.String("teleporter", "", "TeleporterMessengerV2 address (same on both chains) (required)")
	teleporterArtifact := flag.String("teleporter-abi", "", "path to TeleporterMessengerV2.json for the ABI (required)")
	validatorList := flag.String("validators", "", "comma-separated primary-network validator staking addresses (required)")
	besuKeyHex := flag.String("besu-key", "", "funded external-chain private key, hex (required)")
	flag.Parse()

	for name, v := range map[string]string{
		"tx": *txHashHex, "teleporter": *teleporterStr, "teleporter-abi": *teleporterArtifact,
		"validators": *validatorList, "besu-key": *besuKeyHex,
	} {
		if v == "" {
			log.Fatalf("--%s is required", name)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	teleporterAddr := common.HexToAddress(*teleporterStr)

	// ---- 1. Read the C-Chain tx: warp message + Teleporter struct ----
	unsigned, teleporterMsg, err := fetchCChainSend(ctx, *avalancheURI, *txHashHex, teleporterAddr)
	if err != nil {
		log.Fatalf("read C-Chain send tx: %v", err)
	}
	networkID := unsigned.NetworkID
	cChainID := unsigned.SourceChainID
	log.Printf("warp message: source C-Chain %s, %d payload bytes, nonce %s",
		cChainID, len(unsigned.Payload), teleporterMsg.MessageNonce)

	// ---- 2. Canonical primary-network validator set ----
	pClient := platformvm.NewClient(*avalancheURI)
	pHeight, err := pClient.GetHeight(ctx)
	if err != nil {
		log.Fatalf("P-Chain height: %v", err)
	}
	vdrMap, err := pClient.GetValidatorsAt(ctx, constants.PrimaryNetworkID, platformapi.Height(pHeight))
	if err != nil {
		log.Fatalf("primary validators: %v", err)
	}
	warpSet, err := validators.FlattenValidatorSet(vdrMap)
	if err != nil {
		log.Fatalf("flatten set: %v", err)
	}
	log.Printf("primary network: %d validators, total weight %d", len(warpSet.Validators), warpSet.TotalWeight)

	// ---- 3. ACP-118 signature requests to the primary validators ----
	// On-chain warp messages need no justification: each node signs anything
	// its C-Chain warp backend has stored.
	prefix := p2p.ProtocolPrefix(acp118.HandlerID)
	sigs := collectSignatures(ctx, networkID, cChainID, prefix, unsigned, nil, strings.Split(*validatorList, ","))

	// ---- 4. Verify + aggregate to quorum ----
	signerBits := set.NewBits()
	var validSigs []*bls.Signature
	var signedWeight uint64
	for nodeID, sigBytes := range sigs {
		sig, err := bls.SignatureFromBytes(sigBytes)
		if err != nil {
			log.Printf("validator %s: bad signature: %v", nodeID, err)
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
			log.Printf("validator %s: not in set or signature invalid — ignoring", nodeID)
			continue
		}
		if signerBits.Contains(idx) {
			continue
		}
		signerBits.Add(idx)
		validSigs = append(validSigs, sig)
		signedWeight += warpSet.Validators[idx].Weight
		log.Printf("validator %s: verified (index %d)", nodeID, idx)
	}
	if signedWeight*100 < warpSet.TotalWeight*67 {
		log.Fatalf("QUORUM NOT REACHED: %d/%d weight (%d%%)", signedWeight, warpSet.TotalWeight, signedWeight*100/warpSet.TotalWeight)
	}
	agg, err := bls.AggregateSignatures(validSigs)
	if err != nil {
		log.Fatalf("aggregate: %v", err)
	}
	log.Printf("quorum reached: %d/%d validators, %d%% weight",
		len(validSigs), len(warpSet.Validators), signedWeight*100/warpSet.TotalWeight)

	// Registry attestation format: raw signers bitset || uncompressed (192-byte) BLS signature.
	attestation := append(signerBits.Bytes(), agg.Serialize()...)

	// ---- 5. Deliver on the external chain ----
	receipt, err := deliver(ctx, *besuRPC, *teleporterArtifact, *besuKeyHex, teleporterAddr,
		teleporterMsg, networkID, cChainID, attestation)
	if err != nil {
		log.Fatalf("delivery failed: %v", err)
	}
	if receipt.Status != 1 {
		log.Fatalf("receiveCrossChainMessage reverted on the external chain (tx %s)", receipt.TxHash)
	}
	log.Printf("delivered in external-chain block %d (tx %s)", receipt.BlockNumber, receipt.TxHash)

	fmt.Printf("\nDELIVERED: a C-Chain Teleporter message, signed by the primary-network\n")
	fmt.Printf("validators, verified ON-CHAIN by the SubsetUpdater registry (EIP-2537 BLS)\n")
	fmt.Printf("and accepted by the same-address stock TeleporterMessengerV2 on the external chain\n")
}
