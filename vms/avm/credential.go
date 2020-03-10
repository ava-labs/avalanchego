// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"errors"

	"github.com/ava-labs/gecko/vms/components/verify"
)

var (
	errNilCredential   = errors.New("nil credential is not valid")
	errNilFxCredential = errors.New("nil feature extension credential is not valid")
)

// Credential ...
type Credential struct {
	Cred verify.Verifiable `serialize:"true"`
}

// Credential returns the feature extension credential that this Credential is
// using.
func (cred *Credential) Credential() verify.Verifiable { return cred.Cred }

// Verify implements the verify.Verifiable interface
func (cred *Credential) Verify() error {
	switch {
	case cred == nil:
		return errNilCredential
	case cred.Cred == nil:
		return errNilFxCredential
	default:
		return cred.Cred.Verify()
	}
}
