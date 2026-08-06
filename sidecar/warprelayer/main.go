// Command warprelayer delivers a Teleporter message between two Avalanche
// chains, for example an L1 -> the Fuji C-Chain. The path is pure stock: the
// own validators of the source L1 sign via ACP-118, and the warp precompile
// of the destination chain verifies against the P-Chain-registered set.
// There is no committee and there are no forks.
//
// The flow has these steps:
//   - Read the SendWarpMessage and SendCrossChainMessage logs of the source
//     tx.
//   - Fetch the validator set of the source subnet (GetValidatorsAt on a
//     full node).
//   - Collect ACP-118 signatures.
//   - Aggregate the signatures to quorum.
//   - Deliver via stock receiveCrossChainMessage with the signed message as
//     a warp predicate.
package main

import (
	"context"
	"flag"
	"log"
	"math/big"
	"strings"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/sidecar/internal/relayer"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	platformapi "github.com/ava-labs/avalanchego/vms/platformvm/api"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"

	"github.com/ava-labs/libevm/accounts/abi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethclient"
)

func main() {
	fullNodeURI := flag.String("full-node-uri", "", "URI of a full node tracking the source subnet, for GetValidatorsAt (required)")
	sourceRPC := flag.String("source-rpc", "", "source chain JSON-RPC (required)")
	destRPC := flag.String("dest-rpc", "", "destination chain JSON-RPC (required)")
	txHashHex := flag.String("tx", "", "source-chain tx hash of the Teleporter send (required)")
	teleporterStr := flag.String("teleporter", "", "TeleporterMessengerV2 address (same on both chains) (required)")
	teleporterArtifact := flag.String("teleporter-abi", "", "path to TeleporterMessengerV2.json (required)")
	subnetStr := flag.String("subnet", "", "source subnet ID (required)")
	validatorList := flag.String("validators", "", "comma-separated source-validator staking addresses (required)")
	ethKeyStr := flag.String("eth-key", "", "funded destination-chain key (PrivateKey-... or hex) (required)")
	flag.Parse()
	for name, v := range map[string]string{
		"full-node-uri": *fullNodeURI, "source-rpc": *sourceRPC, "dest-rpc": *destRPC,
		"tx": *txHashHex, "teleporter": *teleporterStr, "teleporter-abi": *teleporterArtifact,
		"subnet": *subnetStr, "validators": *validatorList, "eth-key": *ethKeyStr,
	} {
		if v == "" {
			log.Fatalf("--%s is required", name)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	subnetID, err := ids.FromString(*subnetStr)
	if err != nil {
		log.Fatalf("parse subnet: %v", err)
	}
	teleporterAddr := common.HexToAddress(*teleporterStr)

	// ---- 1. Source tx: unsigned warp message + Teleporter struct ----
	unsigned, teleporterMsg, err := relayer.FetchTeleporterSend(ctx, *sourceRPC, *txHashHex, teleporterAddr)
	if err != nil {
		log.Fatalf("read source send tx: %v", err)
	}
	log.Printf("warp message: source %s, %d payload bytes, nonce %s",
		unsigned.SourceChainID, len(unsigned.Payload), teleporterMsg.MessageNonce)

	// ---- 2. Source subnet's validator set (canonical order) ----
	pClient := platformvm.NewClient(*fullNodeURI)
	vdrMap, err := pClient.GetValidatorsAt(ctx, subnetID, platformapi.ProposedHeight)
	if err != nil {
		log.Fatalf("GetValidatorsAt: %v", err)
	}
	warpSet, err := validators.FlattenValidatorSet(vdrMap)
	if err != nil {
		log.Fatalf("flatten set: %v", err)
	}
	log.Printf("source subnet: %d validators, total weight %d", len(warpSet.Validators), warpSet.TotalWeight)

	// ---- 3. ACP-118 signatures from the source validators ----
	prefix := p2p.ProtocolPrefix(acp118.HandlerID)
	sigs := relayer.CollectSignatures(ctx, unsigned.NetworkID, unsigned.SourceChainID,
		prefix, unsigned, nil, strings.Split(*validatorList, ","))

	// ---- 4. Verify + aggregate ----
	signerBits, agg, _, pct, err := relayer.VerifyAndAggregate(warpSet, sigs, unsigned, "validator")
	if err != nil {
		log.Fatalf("%v", err)
	}
	bitSig := &avalancheWarp.BitSetSignature{Signers: signerBits.Bytes()}
	copy(bitSig.Signature[:], bls.SignatureToBytes(agg))
	signedMsg, err := avalancheWarp.NewMessage(unsigned, bitSig)
	if err != nil {
		log.Fatalf("signed message: %v", err)
	}
	log.Printf("quorum reached: %d/%d validators, %.0f%% weight", signerBits.Len(), len(warpSet.Validators), pct)

	// ---- 5. Deliver with the signed message as a warp predicate ----
	teleporterABI := relayer.MustLoadABI(*teleporterArtifact)
	uint32T, _ := abi.NewType("uint32", "", nil)
	attestation, _ := abi.Arguments{{Type: uint32T}}.Pack(uint32(0)) // warp index 0 (WarpAdapter format)
	icm := relayer.TeleporterICMMessage{
		Message:            *teleporterMsg,
		SourceNetworkID:    unsigned.NetworkID,
		SourceBlockchainID: [32]byte(unsigned.SourceChainID),
		Attestation:        attestation,
	}
	callData, err := teleporterABI.Pack("receiveCrossChainMessage", icm, common.Address{})
	if err != nil {
		log.Fatalf("pack receiveCrossChainMessage: %v", err)
	}
	var fundedKey secp256k1.PrivateKey
	if err := fundedKey.UnmarshalText([]byte(*ethKeyStr)); err != nil {
		log.Fatalf("parse eth key: %v", err)
	}
	ethKey := fundedKey.ToECDSA()
	client, err := ethclient.Dial(*destRPC)
	if err != nil {
		log.Fatal(err)
	}
	chainID, err := client.ChainID(ctx)
	if err != nil {
		log.Fatal(err)
	}
	sender := crypto.PubkeyToAddress(ethKey.PublicKey)
	nonce, err := client.PendingNonceAt(ctx, sender)
	if err != nil {
		log.Fatal(err)
	}
	tx := types.MustSignNewTx(ethKey, types.LatestSignerForChainID(chainID), &types.DynamicFeeTx{
		ChainID: chainID, Nonce: nonce, GasTipCap: big.NewInt(1_000_000_000), GasFeeCap: big.NewInt(30_000_000_000),
		Gas: 2_500_000, To: &teleporterAddr, Data: callData,
		AccessList: relayer.BuildPredicate(signedMsg.Bytes()),
	})
	if err := client.SendTransaction(ctx, tx); err != nil {
		log.Fatalf("send receive tx: %v", err)
	}
	receipt, err := relayer.WaitReceipt(ctx, client, tx.Hash())
	if err != nil {
		log.Fatal(err)
	}
	if receipt.Status != 1 {
		log.Fatalf("receiveCrossChainMessage reverted (tx %s)", tx.Hash())
	}
	log.Printf("DELIVERED in destination block %d (tx %s) — stock warp, signed by the source L1's own validators", receipt.BlockNumber, tx.Hash())
}
