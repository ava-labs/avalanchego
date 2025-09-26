// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ledger

import (
	"fmt"
	"strings"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/keychain"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/version"

	ledger "github.com/ava-labs/ledger-avalanche-go"
	bip32 "github.com/tyler-smith/go-bip32"
)

const (
	rootPath          = "m/44'/9000'/0'" // BIP44: m / purpose' / coin_type' / account'
	ledgerBufferLimit = 8192
	ledgerPathSize    = 9
	maxRetries        = 5
	initialRetryDelay = 200 * time.Millisecond
)

var _ keychain.Ledger = (*Ledger)(nil)

// retryOnHIDAPIError executes a function up to maxRetries times if it encounters
// the specific "hidapi: unknown failure" error or APDU error 0x6987
func retryOnHIDAPIError(fn func() error) error {
	var err error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}

		// Check if the error contains the specific HIDAPI failure message or APDU 0x6987
		if strings.Contains(err.Error(), "hidapi: unknown failure") ||
			strings.Contains(err.Error(), "APDU Error Code from Ledger Device: 0x6987") {
			if attempt < maxRetries {
				// Calculate backoff delay: 200ms, 400ms, 600ms, 800ms
				delay := time.Duration(attempt) * initialRetryDelay
				time.Sleep(delay)
				continue
			}
		}

		// If it's not a retryable error or we've exhausted retries, return the error
		return err
	}
	return err
}

// Ledger is a wrapper around the low-level Ledger Device interface that
// provides Avalanche-specific access.
type Ledger struct {
	device *ledger.LedgerAvalanche
	epk    *bip32.Key
}

func New() (*Ledger, error) {
	var device *ledger.LedgerAvalanche
	err := retryOnHIDAPIError(func() error {
		var err error
		device, err = ledger.FindLedgerAvalancheApp()
		return err
	})
	return &Ledger{
		device: device,
	}, err
}

func addressPath(index uint32) string {
	return fmt.Sprintf("%s/0/%d", rootPath, index)
}

func (l *Ledger) Address(hrp string, addressIndex uint32) (ids.ShortID, error) {
	var resp *ledger.ResponseAddr
	err := retryOnHIDAPIError(func() error {
		var err error
		resp, err = l.device.GetPubKey(addressPath(addressIndex), false, hrp, "")
		return err
	})
	if err != nil {
		return ids.ShortEmpty, err
	}
	return ids.ToShortID(resp.Hash)
}

func (l *Ledger) Addresses(addressIndices []uint32) ([]ids.ShortID, error) {
	if l.epk == nil {
		var pk []byte
		var chainCode []byte
		err := retryOnHIDAPIError(func() error {
			var err error
			pk, chainCode, err = l.device.GetExtPubKey(rootPath, false, "", "")
			return err
		})
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
	var response *ledger.ResponseSign
	err := retryOnHIDAPIError(func() error {
		var err error
		response, err = l.device.SignHash(rootPath, strIndices, hash)
		return err
	})
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
	var response *ledger.ResponseSign
	err := retryOnHIDAPIError(func() error {
		var err error
		response, err = l.device.Sign(rootPath, strIndices, txBytes, strIndices)
		return err
	})
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
	var resp *ledger.VersionInfo
	err := retryOnHIDAPIError(func() error {
		var err error
		resp, err = l.device.GetVersion()
		return err
	})
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
	return retryOnHIDAPIError(func() error {
		return l.device.Close()
	})
}
