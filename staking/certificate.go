// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import (
	"crypto/x509"
)

type Certificate struct {
	Raw                []byte
	PublicKey          any
	SignatureAlgorithm x509.SignatureAlgorithm
}
