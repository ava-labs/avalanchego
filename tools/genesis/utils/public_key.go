// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/ethereum/go-ethereum/crypto"
)

func PublicKeyFromString(publicKey string) (*secp256k1.PublicKey, error) {
	publicKey = strings.TrimSpace(strings.TrimPrefix(publicKey, "0x"))
	pkBytes, err := hex.DecodeString(publicKey)
	if err != nil {
		return nil, fmt.Errorf("could not parse public key bytes %v", publicKey)
	}
	return secp256k1.ParsePubKey(pkBytes)
}

func ToPAddress(publicKey *secp256k1.PublicKey) (ids.ShortID, error) {
	addrBytes := hashing.PubkeyBytesToAddress(publicKey.SerializeCompressed())
	return ids.ToShortID(addrBytes)
}

func ToCAddress(publicKey *secp256k1.PublicKey) (ids.ShortID, error) {
	pKey, err := crypto.UnmarshalPubkey(publicKey.SerializeUncompressed())
	if err != nil {
		return ids.ShortEmpty, fmt.Errorf("could not parse public key %w", err)
	}

	addr := crypto.PubkeyToAddress(*pKey)
	return ids.ToShortID(addr.Bytes())
}
