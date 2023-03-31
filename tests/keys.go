// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"bufio"
	"encoding/hex"
	"os"
	"strings"

	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
)

// Loads a list of secp256k1 hex-encoded private keys from the file, new-line separated.
func LoadHexTestKeys(filePath string) (keys []*secp256k1.PrivateKey, err error) {
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

var keyFactory = new(secp256k1.Factory)

func decodeHexPrivateKey(enc string) (*secp256k1.PrivateKey, error) {
	rawPk := strings.Replace(enc, secp256k1.PrivateKeyPrefix, "", 1)
	skBytes, err := hex.DecodeString(rawPk)
	if err != nil {
		return nil, err
	}
	return keyFactory.ToPrivateKey(skBytes)
}
