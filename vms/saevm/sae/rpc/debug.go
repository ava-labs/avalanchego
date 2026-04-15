// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

func (b *backend) SetHead(uint64) {
	b.Logger().Info("debug_setHead called but not supported by SAE")
}
