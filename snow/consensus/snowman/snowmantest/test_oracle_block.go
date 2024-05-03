// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowmantest

import (
	"context"

	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
)

var (
	_ snowman.Block       = (*OracleBlock)(nil)
	_ snowman.OracleBlock = (*OracleBlock)(nil)
)

type OracleBlock struct {
	*Block
	OptionsV [2]snowman.Block
}

func (t *OracleBlock) Options(context.Context) ([2]snowman.Block, error) {
	return t.OptionsV, nil
}
