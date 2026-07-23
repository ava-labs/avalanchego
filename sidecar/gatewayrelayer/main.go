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
	"os"
	"strings"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethclient"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/oracle"
	"github.com/ava-labs/avalanchego/sidecar/internal/relayer"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	platformapi "github.com/ava-labs/avalanchego/vms/platformvm/api"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	warppayload "github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

func main() {
	avalancheURI := flag.String("avalanche-uri", "", "Avalanche node API URI (required; tmpnet assigns ports per network — see ~/.tmpnet/networks/latest/*/process.json)")
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

	if *avalancheURI == "" {
		log.Fatalf("--avalanche-uri is required")
	}

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
	height, payload, err := relayer.FetchLog(ctx, *besuRPC, *txHashHex, common.HexToAddress(*contract))
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
		len(warpSet.Validators), warpSet.TotalWeight, relayer.QuorumNumerator)

	// ---- 4. Request signatures from each attestor over ACP-118 ----
	sigs := relayer.CollectSignatures(ctx, networkID, gatewayChainID,
		p2p.ProtocolPrefix(p2p.OracleSignatureRequestHandlerID),
		unsigned, txHash, strings.Split(*attestorList, ","))

	// ---- 5. Verify each signature and build the quorum aggregate ----
	signerBits, agg, signedWeight, _, err := relayer.VerifyAndAggregate(warpSet, sigs, unsigned, "attestor")
	if err != nil {
		log.Fatalf("%v", err)
	}
	bitSig := &avalancheWarp.BitSetSignature{Signers: signerBits.Bytes()}
	copy(bitSig.Signature[:], bls.SignatureToBytes(agg))
	signedMsg, err := avalancheWarp.NewMessage(unsigned, bitSig)
	if err != nil {
		log.Fatalf("assemble signed message: %v", err)
	}
	log.Printf("quorum reached: %d/%d attestors, weight %d/%d (%d%%)",
		signerBits.Len(), len(warpSet.Validators), signedWeight, warpSet.TotalWeight,
		signedWeight*100/warpSet.TotalWeight)

	// ---- 6. Deliver to the destination chain ----
	if err := deliver(ctx, *avalancheURI, *proverArtifact, *ethKeyStr, signedMsg); err != nil {
		log.Fatalf("delivery failed: %v", err)
	}
	fmt.Printf("\nDELIVERED: event from the external chain, verified by %d independent attestors,\n", signerBits.Len())
	fmt.Printf("accepted by the destination chain's stock warp verification as a message from\n")
	fmt.Printf("Virtual L1 %s\n", gatewayChainID)
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
	deployReceipt, err := relayer.WaitReceipt(ctx, client, deployTx.Hash())
	if err != nil || deployReceipt.Status != 1 {
		return fmt.Errorf("prover deploy failed: %v", err)
	}
	prover := deployReceipt.ContractAddress

	callData := append(crypto.Keccak256([]byte("prove(uint32)"))[:4], make([]byte, 32)...)

	proveTx := types.MustSignNewTx(ethKey, signer, &types.DynamicFeeTx{
		ChainID: chainID, Nonce: nonce + 1, GasTipCap: gasTip, GasFeeCap: gasFee,
		Gas: 5_000_000, To: &prover, Data: callData,
		AccessList: relayer.BuildPredicate(signedMsg.Bytes()),
	})
	if err := client.SendTransaction(ctx, proveTx); err != nil {
		return fmt.Errorf("send prove tx: %w", err)
	}
	receipt, err := relayer.WaitReceipt(ctx, client, proveTx.Hash())
	if err != nil {
		return err
	}
	if receipt.Status != 1 {
		return fmt.Errorf("prove tx reverted — destination did not verify the message (tx %s)", proveTx.Hash())
	}
	log.Printf("delivered in destination block %d (tx %s, prover %s)", receipt.BlockNumber, proveTx.Hash(), prover)
	return nil
}
