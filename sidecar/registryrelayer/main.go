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

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/sidecar/internal/relayer"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	platformapi "github.com/ava-labs/avalanchego/vms/platformvm/api"

	"github.com/ava-labs/libevm/common"
)

func main() {
	avalancheURI := flag.String("avalanche-uri", "", "Avalanche node API URI (required; tmpnet assigns ports per network — see ~/.tmpnet/networks/latest/*/process.json)")
	besuRPC := flag.String("besu-rpc", "http://127.0.0.1:9545", "external EVM chain JSON-RPC endpoint")
	txHashHex := flag.String("tx", "", "C-Chain tx hash of the Teleporter send (required)")
	teleporterStr := flag.String("teleporter", "", "TeleporterMessengerV2 address (same on both chains) (required)")
	teleporterArtifact := flag.String("teleporter-abi", "", "path to TeleporterMessengerV2.json for the ABI (required)")
	validatorList := flag.String("validators", "", "comma-separated primary-network validator staking addresses (default: discovered via the info API on --avalanche-uri)")
	besuKeyHex := flag.String("besu-key", "", "funded external-chain private key, hex (required)")
	flag.Parse()

	if *avalancheURI == "" {
		log.Fatalf("--avalanche-uri is required")
	}

	for name, v := range map[string]string{
		"tx": *txHashHex, "teleporter": *teleporterStr, "teleporter-abi": *teleporterArtifact,
		"besu-key": *besuKeyHex,
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
	// Staking addresses come from --validators when given; otherwise they are
	// discovered from the queried node (itself + its peers, filtered to the
	// primary validator set) — tmpnet assigns them randomly per network, so a
	// hardcoded default would never be right.
	var validatorAddrs []string
	if *validatorList != "" {
		validatorAddrs = strings.Split(*validatorList, ",")
	} else {
		validatorAddrs, err = discoverPrimaryValidators(ctx, *avalancheURI, warpSet)
		if err != nil {
			log.Fatalf("discover primary validators: %v", err)
		}
		log.Printf("discovered %d primary validator staking addresses", len(validatorAddrs))
	}
	// On-chain warp messages need no justification: each node signs anything
	// its C-Chain warp backend has stored.
	prefix := p2p.ProtocolPrefix(acp118.HandlerID)
	sigs := relayer.CollectSignatures(ctx, networkID, cChainID, prefix, unsigned, nil, validatorAddrs)

	// ---- 4. Verify + aggregate to quorum ----
	signerBits, agg, _, pct, err := relayer.VerifyAndAggregate(warpSet, sigs, unsigned, "validator")
	if err != nil {
		log.Fatalf("%v", err)
	}
	log.Printf("quorum reached: %d/%d validators, %.0f%% weight",
		signerBits.Len(), len(warpSet.Validators), pct)

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

// discoverPrimaryValidators returns staking addresses for the primary-network
// validators reachable from the given node: the node itself plus its peers,
// filtered to the validator set's node IDs.
func discoverPrimaryValidators(
	ctx context.Context,
	avalancheURI string,
	warpSet validators.WarpSet,
) ([]string, error) {
	primary := set.Set[ids.NodeID]{}
	for _, v := range warpSet.Validators {
		for _, id := range v.NodeIDs {
			primary.Add(id)
		}
	}

	infoClient := info.NewClient(avalancheURI)
	var addrs []string
	selfID, _, err := infoClient.GetNodeID(ctx)
	if err != nil {
		return nil, fmt.Errorf("info.getNodeID: %w", err)
	}
	if primary.Contains(selfID) {
		ip, err := infoClient.GetNodeIP(ctx)
		if err != nil {
			return nil, fmt.Errorf("info.getNodeIP: %w", err)
		}
		addrs = append(addrs, ip.String())
	}
	peers, err := infoClient.Peers(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("info.peers: %w", err)
	}
	for _, p := range peers {
		if !primary.Contains(p.ID) {
			continue
		}
		addr := p.PublicIP
		if !addr.IsValid() {
			addr = p.IP
		}
		addrs = append(addrs, addr.String())
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("no primary validators reachable from %s", avalancheURI)
	}
	return addrs, nil
}
