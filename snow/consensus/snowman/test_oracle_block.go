// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"
)

var (
	_ Block       = (*TestOracleBlock)(nil)
	_ OracleBlock = (*TestOracleBlock)(nil)
)

type TestOracleBlock struct {
	*TestBlock
	OptionsV [2]Block
}

func (t *TestOracleBlock) Options(context.Context) ([2]Block, error) {
	return t.OptionsV, nil
}
