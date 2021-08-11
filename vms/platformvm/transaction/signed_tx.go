package transaction

import (
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/platformcodec"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// SignedTx is a signed transaction
type SignedTx struct {
	// The body of this transaction
	UnsignedTx `serialize:"true" json:"unsignedTx"`

	// The credentials of this transaction
	Creds []verify.Verifiable `serialize:"true" json:"credentials"`
}

// Sign this transaction with the provided signers
func (tx *SignedTx) Sign(c codec.Manager, signers [][]*crypto.PrivateKeySECP256K1R) error {
	unsignedBytes, err := c.Marshal(platformcodec.Version, &tx.UnsignedTx)
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

	signedBytes, err := c.Marshal(platformcodec.Version, tx)
	if err != nil {
		return fmt.Errorf("couldn't marshal ProposalTx: %w", err)
	}
	tx.Initialize(unsignedBytes, signedBytes)
	return nil
}
