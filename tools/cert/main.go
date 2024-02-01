// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"flag"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/cb58"
	utilsSecp256k1 "github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

var (
	keyFile    = "staker%s.key"
	certFile   = "staker%s.crt"
	destPath   = "./"
	privateKey = ""
)

func main() {
	count := 1
	flag.StringVar(&destPath, "destPath", destPath, "Destination path")
	flag.StringVar(&privateKey, "nodepk", privateKey, "NodeID privateKey")
	flag.IntVar(&count, "count", 1, "Number of certificates")
	flag.Parse()

	var pk *secp256k1.PrivateKey
	if privateKey != "" {
		if count != 1 {
			fmt.Printf("parameter count not allowed if nodepk provided")
			os.Exit(1)
		}
		if !strings.HasPrefix(privateKey, utilsSecp256k1.PrivateKeyPrefix) {
			fmt.Printf("prefixed private key expected")
			os.Exit(1)
		}
		keyBytes, err := cb58.Decode(privateKey[len(utilsSecp256k1.PrivateKeyPrefix):])
		if err != nil {
			fmt.Printf("cannot decode private key (%v)", err)
			os.Exit(1)
		}
		pk = secp256k1.PrivKeyFromBytes(keyBytes)
	}

	num := ""
	for i := 1; i <= count; i++ {
		if count > 1 {
			num = fmt.Sprintf("%d", i)
		}

		keyPath := path.Join(destPath, fmt.Sprintf(keyFile, num))
		certPath := path.Join(destPath, fmt.Sprintf(certFile, num))

		err := staking.InitNodeStakingKeyPair(keyPath, certPath, pk)
		if err != nil {
			fmt.Printf("couldn't create certificate files: %s\n", err)
			os.Exit(1)
		}

		cert, err := staking.LoadTLSCertFromFiles(keyPath, certPath)
		if err != nil {
			fmt.Printf("couldn't read staking certificate: %s\n", err)
			os.Exit(1)
		}

		id, err := peer.CertToID(cert.Leaf)
		if err != nil {
			fmt.Printf("cannot extract nodeID from certificate: %s\n", err)
			os.Exit(1)
		}
		fmt.Printf("NodeID%s: %s\n", num, id.String())
	}
}
