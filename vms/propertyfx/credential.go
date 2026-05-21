// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package propertyfx

import "github.com/MetalBlockchain/metalgo/vms/secp256k1fx"

type Credential struct {
	secp256k1fx.Credential `serialize:"true"`
}
