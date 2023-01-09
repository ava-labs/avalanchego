// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import "errors"

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

// Verify verifies against each verifier.
//
// Every signature that is provided needs to pass verification, but not all
// signatures need to be present (i.e a peer doesn't necessarily have to provide
// every type of signature, but it better not lie about the ones it does choose
// to provide).
func (i IPVerifiers) Verify(ipBytes []byte, sig Signature) error {
	// Every signature that is provided needs to pass verification,
	// but a missing signature doesn't ne
	for verifier, required := range i.verifiers {
		err := verifier.Verify(ipBytes, sig)
		if errors.Is(err, errMissingSignature) && !required {
			continue
		}
		if err != nil {
			return err
		}
	}

	return nil
}
