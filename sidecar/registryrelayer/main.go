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
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm"

	"github.com/ava-labs/libevm/common"
)

func main() {
	avalancheURI := flag.String("avalanche-uri", "", "Avalanche node API URI (required; tmpnet assigns ports per network — see ~/.tmpnet/networks/latest/*/process.json)")
	besuRPC := flag.String("besu-rpc", "http://127.0.0.1:9545", "external EVM chain JSON-RPC endpoint")
	txHashHex := flag.String("tx", "", "C-Chain tx hash of the Teleporter send (required)")
	teleporterStr := flag.String("teleporter", "", "TeleporterMessengerV2 address (same on both chains) (required)")
	registryStr := flag.String("registry", "", "SubsetUpdater registry address on the external chain (required)")
	teleporterArtifact := flag.String("teleporter-abi", "", "path to TeleporterMessengerV2.json for the ABI (required)")
	validatorList := flag.String("validators", "", "comma-separated primary-network validator staking addresses (default: discovered via the info API on --avalanche-uri)")
	besuKeyHex := flag.String("besu-key", "", "funded external-chain private key, hex (required)")
	sourceRPC := flag.String("source-rpc", "", "source chain RPC (default <avalanche-uri>/ext/bc/C/rpc)")
	flag.Parse()
	sourceRPCOverride = *sourceRPC

	if *avalancheURI == "" {
		log.Fatalf("--avalanche-uri is required")
	}

	for name, v := range map[string]string{
		"tx": *txHashHex, "teleporter": *teleporterStr, "registry": *registryStr,
		"teleporter-abi": *teleporterArtifact, "besu-key": *besuKeyHex,
	} {
		if v == "" {
			log.Fatalf("--%s is required", name)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
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

	// ---- 2. Current primary-network validators (key -> NodeID map only) ----
	// getCurrentValidators is served by full nodes (unlike getValidatorsAt,
	// which the public API rejects and a partial-sync node can't answer); we
	// only need it to map each registered BLS key to the NodeID(s) serving it,
	// so signature responses can be credited to the right registered index.
	pClient := platformvm.NewClient(*avalancheURI)
	curVdrs, err := pClient.GetCurrentValidators(ctx, constants.PrimaryNetworkID, nil)
	if err != nil {
		log.Fatalf("current validators: %v", err)
	}
	log.Printf("primary network: %d current validators", len(curVdrs))

	// ---- 2b. The registry's REGISTERED set — the array the contract applies
	// the signature bitset to. Indexes, weights, and the quorum denominator
	// must come from here, not the current P-Chain set: primary-set churn
	// after registration would otherwise shift every index and fail
	// verification on the external chain (fail-closed, but bricked).
	reg, err := fetchRegisteredSet(ctx, *besuRPC, common.HexToAddress(*registryStr), [32]byte(cChainID))
	if err != nil {
		log.Fatalf("read registered set: %v", err)
	}
	// Attribute signature responses to stored indexes by mapping each stored
	// key to the node IDs currently serving it. Stored validators no longer
	// in the current set get no node IDs — they cannot sign, and their weight
	// is the drift the quorum check surfaces.
	nodeIDsByKey := make(map[string][]ids.NodeID, len(curVdrs))
	for _, v := range curVdrs {
		if v.Signer == nil {
			continue
		}
		pk, err := bls.PublicKeyFromCompressedBytes(v.Signer.PublicKey[:])
		if err != nil {
			continue
		}
		k := string(bls.PublicKeyToUncompressedBytes(pk))
		nodeIDsByKey[k] = append(nodeIDsByKey[k], v.NodeID)
	}
	regSet := validators.WarpSet{TotalWeight: reg.TotalWeight}
	live := 0
	for i, v := range reg.Validators {
		// The contract stores keys in the EIP-2537 padded G1 encoding (two
		// 64-byte field elements, each left-padded with 16 zero bytes);
		// convert back to the 96-byte uncompressed form the BLS library uses.
		if len(v.BlsPublicKey) != 128 {
			log.Fatalf("registered key %d: unexpected length %d (want 128, EIP-2537 padded)", i, len(v.BlsPublicKey))
		}
		key96 := make([]byte, 0, 96)
		key96 = append(key96, v.BlsPublicKey[16:64]...)
		key96 = append(key96, v.BlsPublicKey[80:128]...)
		pk := bls.PublicKeyFromValidUncompressedBytes(key96)
		if pk == nil {
			log.Fatalf("registered key %d does not parse as a BLS public key", i)
		}
		nodeIDs := nodeIDsByKey[string(key96)]
		if len(nodeIDs) > 0 {
			live++
		}
		regSet.Validators = append(regSet.Validators, &validators.Warp{
			PublicKey:      pk,
			PublicKeyBytes: key96,
			Weight:         v.Weight,
			NodeIDs:        nodeIDs,
		})
	}
	log.Printf("registered set: %d validators @ P-height %d (weight %d), %d still active in the current set",
		len(reg.Validators), reg.PChainHeight, reg.TotalWeight, live)

	// ---- 3. ACP-118 signature requests to the primary validators ----
	// Staking addresses come from --validators when given; otherwise they are
	// discovered from the queried node (itself + its peers, filtered to the
	// primary validator set) — tmpnet assigns them randomly per network, so a
	// hardcoded default would never be right.
	var validatorAddrs []string
	if *validatorList != "" {
		validatorAddrs = strings.Split(*validatorList, ",")
	} else {
		validatorAddrs, err = discoverPrimaryValidators(ctx, *avalancheURI, regSet)
		if err != nil {
			log.Fatalf("discover primary validators: %v", err)
		}
		log.Printf("discovered %d staking addresses for the registered set", len(validatorAddrs))
	}
	// On-chain warp messages need no justification: each node signs anything
	// its C-Chain warp backend has stored.
	prefix := p2p.ProtocolPrefix(acp118.HandlerID)
	sigs := relayer.CollectSignatures(ctx, networkID, cChainID, prefix, unsigned, nil, validatorAddrs)

	// ---- 4. Verify + aggregate to quorum (against the REGISTERED set) ----
	signerBits, agg, _, pct, err := relayer.VerifyAndAggregate(regSet, sigs, unsigned, "validator")
	if err != nil {
		log.Fatalf("%v", err)
	}
	log.Printf("quorum reached: %d/%d registered validators, %.0f%% weight",
		signerBits.Len(), len(regSet.Validators), pct)

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
