// Command subsetprobe plans the validator subset to register on an external
// chain's registry (the Fuji -> Sepolia outbound direction) and fails fast on
// the risky assumption: that enough of those validators actually answer our
// ACP-118 signature requests.
//
// Selection: primary-network validators ranked by stake weight, keyless
// validators excluded, top N taken. Probe: for each selected validator that
// the queried node is peered with, open a staking-port connection and send a
// signature request for a throwaway message — a refusal is a GOOD outcome
// (the node is reachable and its handler responds); only handshake failures
// and timeouts count against reachability. The report says whether 67% of the
// subset's weight is answerable before any registration gas is spent.
//
// Run it against a node we operate (info.peers needs real peer data; public
// API fleets won't do): locally the tmpnet primary, on Fuji one of the
// committee's own nodes once it has bootstrapped.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/sidecar/internal/relayer"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	platformapi "github.com/ava-labs/avalanchego/vms/platformvm/api"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	warppayload "github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

type candidate struct {
	NodeID    ids.NodeID `json:"nodeID"`
	KeyHex    string     `json:"blsPublicKeyHex"` // 96-byte uncompressed
	Weight    uint64     `json:"weight"`
	Peered    bool       `json:"peered"`
	Reachable bool       `json:"reachable"`
}

func main() {
	uri := flag.String("uri", "", "API URI of a node WE operate (required; info.peers must reflect real peering)")
	top := flag.Int("top", 20, "subset size: top-N primary validators by stake weight")
	probe := flag.Bool("probe", true, "probe each peered candidate with a throwaway ACP-118 request")
	outPath := flag.String("out", "", "optional path to write the selected subset as JSON")
	flag.Parse()

	if *uri == "" {
		log.Fatalf("--uri is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// ---- Selection: top-N primary validators by weight, keyed only ----
	pClient := platformvm.NewClient(*uri)
	height, err := pClient.GetHeight(ctx)
	if err != nil {
		log.Fatalf("P-Chain height: %v", err)
	}
	vdrMap, err := pClient.GetValidatorsAt(ctx, constants.PrimaryNetworkID, platformapi.Height(height))
	if err != nil {
		log.Fatalf("primary validators: %v", err)
	}
	var totalPrimaryWeight uint64
	candidates := make([]*candidate, 0, len(vdrMap))
	for nodeID, v := range vdrMap {
		totalPrimaryWeight += v.Weight
		if v.PublicKey == nil {
			continue
		}
		candidates = append(candidates, &candidate{
			NodeID: nodeID,
			KeyHex: hex.EncodeToString(bls.PublicKeyToUncompressedBytes(v.PublicKey)),
			Weight: v.Weight,
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Weight != candidates[j].Weight {
			return candidates[i].Weight > candidates[j].Weight
		}
		return candidates[i].NodeID.Compare(candidates[j].NodeID) < 0
	})
	if len(candidates) > *top {
		candidates = candidates[:*top]
	}
	var subsetWeight uint64
	for _, c := range candidates {
		subsetWeight += c.Weight
	}
	log.Printf("primary network: %d validators, weight %d @ height %d", len(vdrMap), totalPrimaryWeight, height)
	log.Printf("selected subset: %d validators, weight %d (%.1f%% of primary)",
		len(candidates), subsetWeight, 100*float64(subsetWeight)/float64(totalPrimaryWeight))

	// ---- Probe: is each candidate's staking port answerable from here? ----
	if *probe {
		infoClient := info.NewClient(*uri)
		networkID, err := infoClient.GetNetworkID(ctx)
		if err != nil {
			log.Fatalf("network ID: %v", err)
		}
		cChainID, err := fetchCChainID(ctx, pClient)
		if err != nil {
			log.Fatalf("C-Chain ID: %v", err)
		}
		peers, err := infoClient.Peers(ctx, nil)
		if err != nil {
			log.Fatalf("info.peers: %v", err)
		}
		peerAddr := make(map[ids.NodeID]string, len(peers))
		for _, p := range peers {
			addr := p.PublicIP
			if !addr.IsValid() {
				addr = p.IP
			}
			peerAddr[p.ID] = addr.String()
		}
		// Also probe the queried node itself if it is in the subset.
		selfID, _, err := infoClient.GetNodeID(ctx)
		if err == nil {
			if selfIP, err := infoClient.GetNodeIP(ctx); err == nil {
				peerAddr[selfID] = selfIP.String()
			}
		}

		// A throwaway-but-parseable message: the refusal it provokes proves the
		// handler answered. Only handshake failures/timeouts count as unreachable.
		addressedCall, err := warppayload.NewAddressedCall(nil, []byte("subsetprobe"))
		if err != nil {
			log.Fatalf("addressed call: %v", err)
		}
		unsigned, err := avalancheWarp.NewUnsignedMessage(networkID, cChainID, addressedCall.Bytes())
		if err != nil {
			log.Fatalf("unsigned message: %v", err)
		}
		requestPayload, err := proto.Marshal(&sdk.SignatureRequest{Message: unsigned.Bytes()})
		if err != nil {
			log.Fatalf("marshal SignatureRequest: %v", err)
		}
		prefixed := append(p2p.ProtocolPrefix(acp118.HandlerID), requestPayload...)

		var reachable int
		var reachableWeight uint64
		for _, c := range candidates {
			addr, ok := peerAddr[c.NodeID]
			if !ok {
				log.Printf("%s  weight %d  NOT PEERED (cannot probe from this node)", c.NodeID, c.Weight)
				continue
			}
			c.Peered = true
			probeCtx, probeCancel := context.WithTimeout(ctx, 25*time.Second)
			_, _, err := relayer.RequestOne(probeCtx, networkID, cChainID, prefixed, addr)
			probeCancel()
			// A refusal IS a response: the peer handshook and its ACP-118
			// handler processed the request. Only transport-level failures
			// count against reachability.
			if err == nil || strings.Contains(err.Error(), "refused") {
				c.Reachable = true
				reachable++
				reachableWeight += c.Weight
				log.Printf("%s  weight %d  RESPONSIVE (%s)", c.NodeID, c.Weight, addr)
			} else {
				log.Printf("%s  weight %d  UNREACHABLE (%s): %v", c.NodeID, c.Weight, addr, err)
			}
		}

		pctOfSubset := 100 * float64(reachableWeight) / float64(subsetWeight)
		fmt.Printf("\n%d/%d subset validators responsive, %.1f%% of subset weight (quorum needs 67%%)\n",
			reachable, len(candidates), pctOfSubset)
		if pctOfSubset >= 67 {
			fmt.Printf("VERDICT: registering this subset is viable from this vantage point\n")
		} else {
			fmt.Printf("VERDICT: NOT viable — choose different validators, improve peering, or shrink the subset\n")
		}
	}

	if *outPath != "" {
		blob, err := json.MarshalIndent(candidates, "", "  ")
		if err != nil {
			log.Fatalf("marshal subset: %v", err)
		}
		if err := os.WriteFile(*outPath, blob, 0o644); err != nil {
			log.Fatalf("write %s: %v", *outPath, err)
		}
		log.Printf("subset written to %s", *outPath)
	}
}

func fetchCChainID(ctx context.Context, pClient *platformvm.Client) (ids.ID, error) {
	chains, err := pClient.GetBlockchains(ctx)
	if err != nil {
		return ids.Empty, err
	}
	for _, c := range chains {
		if c.Name == "C-Chain" {
			return c.ID, nil
		}
	}
	return ids.Empty, fmt.Errorf("C-Chain not found")
}
