// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

var _ IPSigner = (*IPSigners)(nil)

// IPSigners is a group of signers
type IPSigners struct {
	signers []IPSigner
}

// NewIPSigners returns an instance of IPSigners
func NewIPSigners(signers ...IPSigner) *IPSigners {
	return &IPSigners{
		signers: signers,
	}
}

// Sign signs against each signer and returns the new signed signature.
// Returns an error if signing fails.
func (i IPSigners) Sign(ipBytes []byte, sig Signature) (Signature, error) {
	for _, signer := range i.signers {
		ipSig, err := signer.Sign(ipBytes, sig)
		if err != nil {
			return Signature{}, err
		}
		sig = ipSig
	}

	return sig, nil
}
