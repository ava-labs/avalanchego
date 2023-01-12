// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import "errors"

var errNonExecutableTx = errors.New("this tx type can't be executed, its genesis-only")

func (*BaseTx) Visit(Visitor) error {
	return errNonExecutableTx
}
