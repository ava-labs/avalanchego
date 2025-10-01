// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ledger

import (
	"fmt"

	"github.com/ava-labs/avalanchego/utils/crypto/keychain"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/version"

	ledger "github.com/ava-labs/ledger-avalanche/go"
	bip32 "github.com/tyler-smith/go-bip32"
)

const (
	rootPath          = "m/44'/9000'/0'" // BIP44: m / purpose' / coin_type' / account'
	ledgerBufferLimit = 8192
	ledgerPathSize    = 9
)

var _ keychain.Ledger = (*Ledger)(nil)

// Ledger is a wrapper around the low-level Ledger Device interface that
// provides Avalanche-specific access.
type Ledger struct {
	device *ledger.LedgerAvalanche
	epk    *bip32.Key
}

func New() (keychain.Ledger, error) {
	device, err := ledger.FindLedgerAvalancheApp()
	return &Ledger{
		device: device,
	}, err
}

func addressPath(index uint32) string {
	return fmt.Sprintf("%s/0/%d", rootPath, index)
}

func (l *Ledger) PubKey(addressIndex uint32) (*secp256k1.PublicKey, error) {
	resp, err := l.device.GetPubKey(addressPath(addressIndex), false, "", "")
	if err != nil {
		return nil, err
	}
	pubKey, err := secp256k1.ToPublicKey(resp.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failure parsing public key from ledger: %w", err)
	}
	return pubKey, nil
}

func (l *Ledger) PubKeys(addressIndices []uint32) ([]*secp256k1.PublicKey, error) {
	if l.epk == nil {
		pk, chainCode, err := l.device.GetExtPubKey(rootPath, false, "", "")
		if err != nil {
			return nil, err
		}
		l.epk = &bip32.Key{
			Key:       pk,
			ChainCode: chainCode,
		}
	}
	// derivation path rootPath/0 (BIP44 change level, when set to 0, known as external chain)
	externalChain, err := l.epk.NewChildKey(0)
	if err != nil {
		return nil, err
	}
	pubKeys := make([]*secp256k1.PublicKey, len(addressIndices))
	for i, addressIndex := range addressIndices {
		// derivation path rootPath/0/v (BIP44 address index level)
		address, err := externalChain.NewChildKey(addressIndex)
		if err != nil {
			return nil, err
		}
		pubKey, err := secp256k1.ToPublicKey(address.Key)
		if err != nil {
			return nil, fmt.Errorf("failure parsing public key for ledger child key %d: %w", addressIndex, err)
		}
		pubKeys[i] = pubKey
	}
	return pubKeys, nil
}

func convertToSigningPaths(input []uint32) []string {
	output := make([]string, len(input))
	for i, v := range input {
		output[i] = fmt.Sprintf("0/%d", v)
	}
	return output
}

func (l *Ledger) SignHash(hash []byte, addressIndices []uint32) ([][]byte, error) {
	strIndices := convertToSigningPaths(addressIndices)
	response, err := l.device.SignHash(rootPath, strIndices, hash)
	if err != nil {
		return nil, fmt.Errorf("%w: unable to sign hash", err)
	}
	responses := make([][]byte, len(addressIndices))
	for i, index := range strIndices {
		sig, ok := response.Signature[index]
		if !ok {
			return nil, fmt.Errorf("missing signature %s", index)
		}
		responses[i] = sig
	}
	return responses, nil
}

func (l *Ledger) Sign(txBytes []byte, addressIndices []uint32) ([][]byte, error) {
	// will pass to the ledger addressIndices both as signing paths and change paths
	numSigningPaths := len(addressIndices)
	numChangePaths := len(addressIndices)
	if len(txBytes)+(numSigningPaths+numChangePaths)*ledgerPathSize > ledgerBufferLimit {
		// There is a limit on the tx length that can be parsed by the ledger
		// app. When the tx that is being signed is too large, we sign with hash
		// instead.
		//
		// Ref: https://github.com/ava-labs/avalanche-wallet-sdk/blob/9a71f05e424e06b94eaccf21fd32d7983ed1b040/src/Wallet/Ledger/provider/ZondaxProvider.ts#L68
		unsignedHash := hashing.ComputeHash256(txBytes)
		return l.SignHash(unsignedHash, addressIndices)
	}
	strIndices := convertToSigningPaths(addressIndices)
	response, err := l.device.Sign(rootPath, strIndices, txBytes, strIndices)
	if err != nil {
		return nil, fmt.Errorf("%w: unable to sign transaction", err)
	}
	responses := make([][]byte, len(strIndices))
	for i, index := range strIndices {
		sig, ok := response.Signature[index]
		if !ok {
			return nil, fmt.Errorf("missing signature %s", index)
		}
		responses[i] = sig
	}
	return responses, nil
}

func (l *Ledger) Version() (*version.Semantic, error) {
	resp, err := l.device.GetVersion()
	if err != nil {
		return nil, err
	}
	return &version.Semantic{
		Major: int(resp.Major),
		Minor: int(resp.Minor),
		Patch: int(resp.Patch),
	}, nil
}

func (l *Ledger) Disconnect() error {
	return l.device.Close()
}
