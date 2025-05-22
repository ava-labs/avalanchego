// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

var _ common.Engine = (*Engine)(nil)

type Engine struct {
	common.Handler
	health.Checker
}

func (*Engine) Simplex(nodeID ids.NodeID, _ *p2p.Simplex) error {
	fmt.Println("Simplex message received in the simplex handler!", nodeID)
	return nil
}

func (*Engine) Start(_ context.Context, _ uint32) error {
	return nil
}
