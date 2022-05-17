// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package signed

import (
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// Tx is a signed transaction
type Tx struct {
	// The body of this transaction
	Unsigned unsigned.Tx `serialize:"true" json:"unsignedTx"`

	// The credentials of this transaction
	Creds []verify.Verifiable `serialize:"true" json:"credentials"`
}

// Sign this transaction with the provided signers
func (tx *Tx) Sign(c codec.Manager, signers [][]*crypto.PrivateKeySECP256K1R) error {
	unsignedBytes, err := c.Marshal(unsigned.Version, &tx.Unsigned)
	if err != nil {
		return fmt.Errorf("couldn't marshal UnsignedTx: %w", err)
	}

	// Attach credentials
	hash := hashing.ComputeHash256(unsignedBytes)
	for _, keys := range signers {
		cred := &secp256k1fx.Credential{
			Sigs: make([][crypto.SECP256K1RSigLen]byte, len(keys)),
		}
		for i, key := range keys {
			sig, err := key.SignHash(hash) // Sign hash
			if err != nil {
				return fmt.Errorf("problem generating credential: %w", err)
			}
			copy(cred.Sigs[i][:], sig)
		}
		tx.Creds = append(tx.Creds, cred) // Attach credential
	}

	signedBytes, err := c.Marshal(unsigned.Version, tx)
	if err != nil {
		return fmt.Errorf("couldn't marshal ProposalTx: %w", err)
	}
	tx.Unsigned.Initialize(unsignedBytes, signedBytes)
	return nil
}

// Build signed tx starting from its byte representation
func FromBytes(c codec.Manager, txBytes []byte) (*Tx, error) {
	res := &Tx{}
	if _, err := c.Unmarshal(txBytes, res); err != nil {
		return nil, fmt.Errorf("couldn't parse tx: %w", err)
	}
	unsignedBytes, err := c.Marshal(unsigned.Version, &res.Unsigned)
	if err != nil {
		return nil, fmt.Errorf("couldn't marshal UnsignedTx: %w", err)
	}
	res.Unsigned.Initialize(unsignedBytes, txBytes)
	return res, nil
}
