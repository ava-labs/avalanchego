// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import (
	"io"

	ethtypes "github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
)

var extras = ethtypes.RegisterExtras[
	HeaderExtra, *HeaderExtra,
	ethtypes.NOOPBlockBodyHooks, *ethtypes.NOOPBlockBodyHooks,
	noopStateAccountExtras,
]()

type noopStateAccountExtras struct{}

// EncodeRLP implements the [rlp.Encoder] interface.
func (noopStateAccountExtras) EncodeRLP(w io.Writer) error { return nil }

// DecodeRLP implements the [rlp.Decoder] interface.
func (*noopStateAccountExtras) DecodeRLP(s *rlp.Stream) error { return nil }
