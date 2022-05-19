// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/ava-labs/avalanchego/utils/formatting"
)

// PrivateKeyPrefix is used to denote secret keys rather than other byte arrays.
const PrivateKeyPrefix string = "PrivateKey-"

var EmptyPrivateKey = PrivateKey{}

type PrivateKey []byte

func (pk PrivateKey) String() string {
	// We assume that the maximum size of a byte slice that
	// can be stringified is at least the length of a SECP256K1 private key
	privKeyStr, _ := formatting.EncodeWithChecksum(formatting.CB58, pk.Bytes())
	return PrivateKeyPrefix + privKeyStr
}

func (pk PrivateKey) Bytes() []byte {
	return pk[:]
}

func (pk PrivateKey) MarshalJSON() ([]byte, error) {
	return []byte("\"" + pk.String() + "\""), nil
}

func (pk PrivateKey) MarshalText() ([]byte, error) {
	return []byte(pk.String()), nil
}

func (pk *PrivateKey) UnmarshalJSON(b []byte) error {
	str := string(b)
	if str == nullStr { // If "null", do nothing
		return nil
	} else if len(str) <= 2+len(PrivateKeyPrefix) {
		return fmt.Errorf("expected PrivateKey length to be > %d", 2+len(PrivateKeyPrefix))
	}

	lastIndex := len(str) - 1
	if str[0] != '"' || str[lastIndex] != '"' {
		return errMissingQuotes
	}

	var err error
	*pk, err = PrivateKeyFromString(str[1:lastIndex])
	return err
}

func (pk *PrivateKey) UnmarshalText(text []byte) error {
	return pk.UnmarshalJSON(text)
}

type sortPrivateKeyData []PrivateKey

func (pks sortPrivateKeyData) Less(i, j int) bool {
	return bytes.Compare(
		pks[i].Bytes(),
		pks[j].Bytes()) == -1
}
func (pks sortPrivateKeyData) Len() int      { return len(pks) }
func (pks sortPrivateKeyData) Swap(i, j int) { pks[j], pks[i] = pks[i], pks[j] }

// SortPrivateKeys sorts the private keys lexicographically
func SortPrivateKeys(privateKeys []PrivateKey) {
	sort.Sort(sortPrivateKeyData(privateKeys))
}

// PrivateKeyFromString is the inverse of PrivateKey.String()
func PrivateKeyFromString(privateKeyStr string) (PrivateKey, error) {
	if !strings.HasPrefix(privateKeyStr, PrivateKeyPrefix) {
		return EmptyPrivateKey, fmt.Errorf("private key missing %s prefix", PrivateKeyPrefix)
	}
	trimmedPrivateKey := strings.TrimPrefix(privateKeyStr, PrivateKeyPrefix)
	privKeyBytes, err := formatting.Decode(formatting.CB58, trimmedPrivateKey)
	if err != nil {
		return EmptyPrivateKey, fmt.Errorf("problem parsing private key: %w", err)
	}
	return PrivateKey(privKeyBytes), nil
}
