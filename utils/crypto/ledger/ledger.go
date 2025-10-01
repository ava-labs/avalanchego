// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ledger

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/keychain"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/version"

	ledger "github.com/ava-labs/ledger-avalanche/go"
	bip32 "github.com/tyler-smith/go-bip32"
)

const (
	rootPath = "m/44'/9000'/0'" // BIP44: m / purpose' / coin_type' / account'
	// ledgerBufferLimit corresponds to FLASH_BUFFER_SIZE for Nano S.
	// Modern devices (Nano X, Nano S2, Stax, Flex) support up to 16384 bytes,
	// but we use the conservative limit for universal compatibility.
	//
	// Ref: https://github.com/ava-labs/ledger-avalanche/blob/main/app/src/common/tx.c
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

func (l *Ledger) Address(hrp string, addressIndex uint32) (ids.ShortID, error) {
	resp, err := l.device.GetPubKey(addressPath(addressIndex), true, hrp, "")
	if err != nil {
		return ids.ShortEmpty, err
	}
	return ids.ToShortID(resp.Hash)
}

func (l *Ledger) Addresses(addressIndices []uint32) ([]ids.ShortID, error) {
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
	addresses := make([]ids.ShortID, len(addressIndices))
	for i, addressIndex := range addressIndices {
		// derivation path rootPath/0/v (BIP44 address index level)
		address, err := externalChain.NewChildKey(addressIndex)
		if err != nil {
			return nil, err
		}
		copy(addresses[i][:], hashing.PubkeyBytesToAddress(address.Key))
	}
	return addresses, nil
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
	// We pass addressIndices both as signing paths and change paths.
	// The ledger library deduplicates them, so the buffer contains len(addressIndices) paths.
	// Buffer format: 1 byte (path count) + paths + transaction bytes
	//
	// Ref: https://github.com/ava-labs/ledger-avalanche-go/blob/main/common.go (ConcatMessageAndChangePath)
	bufferSize := 1 + len(addressIndices)*ledgerPathSize + len(txBytes)
	if bufferSize > ledgerBufferLimit {
		// When the tx that is being signed is too large for the ledger buffer,
		// we sign with hash instead.
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
