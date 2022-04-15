// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package crypto

import (
	"github.com/chain4travel/caminogo/ids"
)

type Factory interface {
	NewPrivateKey() (PrivateKey, error)

	ToPublicKey([]byte) (PublicKey, error)
	ToPrivateKey([]byte) (PrivateKey, error)
}

type RecoverableFactory interface {
	Factory

	RecoverPublicKey(message, signature []byte) (PublicKey, error)
	RecoverHashPublicKey(hash, signature []byte) (PublicKey, error)
}

type PublicKey interface {
	Verify(message, signature []byte) bool
	VerifyHash(hash, signature []byte) bool

	Address() ids.ShortID
	Bytes() []byte
}

type PrivateKey interface {
	PublicKey() PublicKey

	Sign(message []byte) ([]byte, error)
	SignHash(hash []byte) ([]byte, error)

	Bytes() []byte
}
