// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package signer

import (
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

var _ Signer = &None{}

type None struct{}

func (*None) Verify() error       { return nil }
func (*None) Key() *bls.PublicKey { return nil }
