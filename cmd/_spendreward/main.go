package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"os"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	pchain "github.com/ava-labs/avalanchego/wallet/chain/p"
)

// Minimal isolation test:
//   Build & sign a BaseTx that consumes a specific reward UTXO (txID:idx),
//   outputs everything-minus-fee to a single address, signed by N keys.
//   If this fails with "UTXO not found", it's a chain/state issue.
//   If it succeeds, the multi-party server has a latent bug.
func main() {
	var (
		uri          = flag.String("uri", "http://127.0.0.1:9660", "P-chain URI")
		utxoTxIDStr  = flag.String("utxo-tx", "", "Reward UTXO source txID (CB58)")
		utxoIdx      = flag.Uint("utxo-idx", 0, "Reward UTXO output index")
		utxoAmt      = flag.Uint64("utxo-amt", 0, "Reward UTXO amount (nAVAX)")
		keysCSV      = flag.String("keys", "", "Comma-separated CB58 private keys (N-of-N)")
		destAddrStr  = flag.String("dest", "", "Bech32 P-chain destination address")
		memoHex      = flag.String("memo", "", "Optional memo hex (changes tx hash)")
		networkID    = flag.Uint("network", 88888, "Network ID")
	)
	flag.Parse()

	if *utxoTxIDStr == "" || *keysCSV == "" || *destAddrStr == "" {
		fmt.Fprintln(os.Stderr, "required: --utxo-tx, --keys, --dest")
		os.Exit(2)
	}

	ctx := context.Background()

	utxoTxID, err := ids.FromString(*utxoTxIDStr)
	must(err, "parse utxo-tx")

	keyStrs := splitCSV(*keysCSV)
	keys := make([]*secp256k1.PrivateKey, len(keyStrs))
	addrs := make([]ids.ShortID, len(keyStrs))
	for i, ks := range keyStrs {
		var k secp256k1.PrivateKey
		must(k.UnmarshalText([]byte(ks)), fmt.Sprintf("key %d", i))
		keys[i] = &k
		addrs[i] = k.Address()
	}
	// Sort addrs to match N-of-N rewardsOwner ordering
	utils.Sort(addrs)

	destPKID, err := address.ParseToID(*destAddrStr)
	must(err, "parse dest")

	pCTX, err := pchain.NewContextFromURI(ctx, *uri)
	must(err, "fetch p-chain context")

	input := &avax.TransferableInput{
		UTXOID: avax.UTXOID{TxID: utxoTxID, OutputIndex: uint32(*utxoIdx)},
		Asset:  avax.Asset{ID: pCTX.AVAXAssetID},
		In: &secp256k1fx.TransferInput{
			Amt:   *utxoAmt,
			Input: secp256k1fx.Input{SigIndices: rangeU32(0, len(addrs))},
		},
	}

	var memoBytes []byte
	if *memoHex != "" {
		memoBytes, err = hex.DecodeString(stripPrefix(*memoHex))
		must(err, "decode memo")
	}

	// First pass: dummy output to compute fee.
	makeOutputs := func(amt uint64) []*avax.TransferableOutput {
		if amt == 0 {
			return []*avax.TransferableOutput{}
		}
		return []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: pCTX.AVAXAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: amt,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{destPKID},
				},
			},
		}}
	}
	feeTx := &txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    uint32(*networkID),
		BlockchainID: constants.PlatformChainID,
		Ins:          []*avax.TransferableInput{input},
		Outs:         makeOutputs(*utxoAmt),
		Memo:         memoBytes,
	}}
	var feeUnsigned txs.UnsignedTx = feeTx
	complexity, err := fee.TxComplexity(feeUnsigned)
	must(err, "complexity")
	gasAmt, err := complexity.ToGas(pCTX.ComplexityWeights)
	must(err, "to gas")
	txFee, err := gasAmt.Cost(pCTX.GasPrice)
	must(err, "cost")
	fmt.Printf("computed fee = %d nAVAX\n", txFee)
	if txFee >= *utxoAmt {
		fmt.Fprintf(os.Stderr, "utxo amt %d too small for fee %d\n", *utxoAmt, txFee)
		os.Exit(1)
	}

	// Build final tx
	baseTx := &txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    uint32(*networkID),
		BlockchainID: constants.PlatformChainID,
		Ins:          []*avax.TransferableInput{input},
		Outs:         makeOutputs(*utxoAmt - txFee),
		Memo:         memoBytes,
	}}

	var unsigned txs.UnsignedTx = baseTx
	unsignedBytes, err := txs.Codec.Marshal(txs.CodecVersion, &unsigned)
	must(err, "marshal unsigned")

	// Sign — create one Credential per input with N sigs.
	hash := hashing.ComputeHash256(unsignedBytes)
	// Map addr -> key to select signature order by sorted addr position
	addrToKey := make(map[ids.ShortID]*secp256k1.PrivateKey, len(keys))
	for _, k := range keys {
		addrToKey[k.Address()] = k
	}
	cred := &secp256k1fx.Credential{
		Sigs: make([][secp256k1.SignatureLen]byte, len(addrs)),
	}
	for i, addr := range addrs {
		sig, err := addrToKey[addr].SignHash(hash)
		must(err, fmt.Sprintf("sign %d", i))
		copy(cred.Sigs[i][:], sig)
	}

	signedTx := &txs.Tx{
		Unsigned: unsigned,
		Creds:    []verify.Verifiable{cred},
	}
	signedBytes, err := txs.Codec.Marshal(txs.CodecVersion, signedTx)
	must(err, "marshal signed")
	signedTx.SetBytes(unsignedBytes, signedBytes)

	fmt.Printf("tx hex: %s\n", hex.EncodeToString(signedBytes))
	fmt.Printf("tx ID:  %s\n", signedTx.TxID)
	fmt.Printf("submit -> %s\n", *uri)

	client := platformvm.NewClient(*uri)
	issuedID, err := client.IssueTx(ctx, signedBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "issue err: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("issued: %s\n", issuedID)
}

func must(err error, msg string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", msg, err)
		os.Exit(1)
	}
}

func splitCSV(s string) []string {
	out := []string{}
	cur := ""
	for _, r := range s {
		if r == ',' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func rangeU32(start, count int) []uint32 {
	out := make([]uint32, count)
	for i := range out {
		out[i] = uint32(start + i)
	}
	return out
}

func stripPrefix(s string) string {
	if len(s) >= 2 && s[:2] == "0x" {
		return s[2:]
	}
	return s
}
