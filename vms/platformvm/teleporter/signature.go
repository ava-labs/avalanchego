// Copyright (C) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package teleporter

import (
	"errors"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

var _ Signature = (*BitSetSignature)(nil)

// TODO: Update this interface when implementing signature verification
type Signature interface {
	Verify() error
}

type BitSetSignature struct {
	Signers   []byte                 `serialize:"true"`
	Signature [bls.SignatureLen]byte `serialize:"true"`
}

func (*BitSetSignature) Verify() error {
	return errors.New("unimplemented")
}
