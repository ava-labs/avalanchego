// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

// NonceState tracks the next usable eth facade nonce per account. The stored
// value is last accepted nonce + 1 (0 for accounts that never transacted), so
// the acceptance rule "nonce strictly greater than last" is "nonce >= next".
type NonceState interface {
	GetNextNonce(addr ids.ShortID) (uint64, error)
	SetNextNonce(addr ids.ShortID, nonce uint64)
}

var (
	_ NonceState = (*State)(nil)
	_ NonceState = (*Diff)(nil)
)

func (s *State) GetNextNonce(addr ids.ShortID) (uint64, error) {
	if nonce, ok := s.modifiedNonces[addr]; ok {
		return nonce, nil
	}
	nonce, err := database.GetUInt64(s.nonceDB, addr[:])
	if err == database.ErrNotFound {
		return 0, nil
	}
	return nonce, err
}

func (s *State) SetNextNonce(addr ids.ShortID, nonce uint64) {
	s.modifiedNonces[addr] = nonce
}

func (s *State) writeNonces() error {
	for addr, nonce := range s.modifiedNonces {
		if err := database.PutUInt64(s.nonceDB, addr[:], nonce); err != nil {
			return err
		}
		delete(s.modifiedNonces, addr)
	}
	return nil
}

func (d *Diff) GetNextNonce(addr ids.ShortID) (uint64, error) {
	if nonce, ok := d.nonces[addr]; ok {
		return nonce, nil
	}
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return 0, ErrMissingParentState
	}
	return parentState.(NonceState).GetNextNonce(addr)
}

func (d *Diff) SetNextNonce(addr ids.ShortID, nonce uint64) {
	if d.nonces == nil {
		d.nonces = make(map[ids.ShortID]uint64)
	}
	d.nonces[addr] = nonce
}

// UTXOIDs merges the parent's address index with this diff's modified UTXOs so
// the eth facade can auto-select inputs against in-flight state. Only on-chain
// UTXOs are visible here; shared memory is never consulted.
// ponytail: previous/limit pagination is ignored, callers pass Empty/MaxInt.
func (d *Diff) UTXOIDs(addr []byte, _ ids.ID, _ int) ([]ids.ID, error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, ErrMissingParentState
	}
	utxoIDs, err := parentState.(avax.UTXOReader).UTXOIDs(addr, ids.Empty, int(^uint(0)>>1))
	if err != nil {
		return nil, err
	}
	merged := make([]ids.ID, 0, len(utxoIDs))
	for _, utxoID := range utxoIDs {
		if utxo, modified := d.modifiedUTXOs[utxoID]; modified && utxo == nil {
			continue // spent in this diff
		}
		merged = append(merged, utxoID)
	}
	for utxoID, utxo := range d.modifiedUTXOs {
		if utxo == nil {
			continue
		}
		addressable, ok := utxo.Out.(avax.Addressable)
		if !ok {
			continue
		}
		for _, utxoAddr := range addressable.Addresses() {
			if string(utxoAddr) == string(addr) {
				merged = append(merged, utxoID)
				break
			}
		}
	}
	return merged, nil
}
