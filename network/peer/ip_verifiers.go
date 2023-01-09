// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

var _ IPVerifier = (*IPVerifiers)(nil)

// IPVerifiers is a group of verifiers
type IPVerifiers struct {
	// map of verifiers => if they are required to pass verification
	verifiers map[IPVerifier]bool
}

// NewIPVerifiers returns a new instance of IPVerifiers
func NewIPVerifiers(verifiers map[IPVerifier]bool) *IPVerifiers {
	return &IPVerifiers{
		verifiers: verifiers,
	}
}

// Verify verifies against each verifier, and fails if any required verifier
// fails verification.
func (i IPVerifiers) Verify(ipBytes []byte, sig Signature) error {
	for verifier, required := range i.verifiers {
		if err := verifier.Verify(ipBytes, sig); required && err != nil {
			return err
		}
	}

	return nil
}
