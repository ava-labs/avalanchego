// Copyright (C) 2022-2025, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

func (tx *BaseTx) Visit(visitor Visitor) error {
	return visitor.BaseTx(tx)
}
