package main

import (
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
)

func main() {
	certPEM, _, err := staking.NewCertAndKeyBytes()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cert/key gen:", err)
		os.Exit(1)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		fmt.Fprintln(os.Stderr, "pem decode failed")
		os.Exit(1)
	}
	cert, err := staking.ParseCertificate(block.Bytes)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse cert:", err)
		os.Exit(1)
	}
	nodeID := ids.NodeIDFromCert(cert)

	ls, err := localsigner.New()
	if err != nil {
		fmt.Fprintln(os.Stderr, "bls signer:", err)
		os.Exit(1)
	}
	pop, err := signer.NewProofOfPossession(ls)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pop:", err)
		os.Exit(1)
	}

	out := map[string]string{
		"nodeID":               nodeID.String(),
		"blsPublicKey":         "0x" + hex.EncodeToString(bls.PublicKeyToCompressedBytes(ls.PublicKey())),
		"blsProofOfPossession": "0x" + hex.EncodeToString(pop.ProofOfPossession[:]),
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}
