// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/ava-labs/avalanchego/utils/crypto"
)

// Loads a list of secp256k1 hex-encoded private keys from the file, new-line separated.
func LoadHexTestKeys(filePath string) (keys []*crypto.PrivateKeySECP256K1R, err error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		s := scanner.Text()
		k, err := decodeHexPrivateKey(s)
		if err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return keys, nil
}

var keyFactory = new(crypto.FactorySECP256K1R)

func decodeHexPrivateKey(enc string) (*crypto.PrivateKeySECP256K1R, error) {
	rawPk := strings.Replace(enc, crypto.PrivateKeyPrefix, "", 1)
	var skBytes []byte
	skBytes, err := hex.DecodeString(rawPk)
	if err != nil {
		return nil, err
	}
	rpk, err := keyFactory.ToPrivateKey(skBytes)
	if err != nil {
		return nil, err
	}
	privKey, ok := rpk.(*crypto.PrivateKeySECP256K1R)
	if !ok {
		return nil, fmt.Errorf("invalid type %T", rpk)
	}
	return privKey, nil
}
