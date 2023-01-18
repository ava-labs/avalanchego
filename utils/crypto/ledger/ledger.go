// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ledger

import (
	"fmt"

	ledger "github.com/ava-labs/ledger-avalanche/go"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/keychain"
	"github.com/ava-labs/avalanchego/version"
)

const rootPath = "m/44'/9000'/0'"

var _ keychain.Ledger = (*Ledger)(nil)

// Ledger is a wrapper around the low-level Ledger Device interface that
// provides Avalanche-specific access.
type Ledger struct {
	device *ledger.LedgerAvalanche
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
	_, hash, err := l.device.GetPubKey(addressPath(addressIndex), true, hrp, "")
	if err != nil {
		return ids.ShortEmpty, err
	}
	return ids.ToShortID(hash)
}

func (l *Ledger) Addresses(addressIndices []uint32) ([]ids.ShortID, error) {
	addresses := make([]ids.ShortID, len(addressIndices))
	for i, v := range addressIndices {
		_, hash, err := l.device.GetPubKey(addressPath(v), false, "", "")
		if err != nil {
			return nil, err
		}
		copy(addresses[i][:], hash)
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
