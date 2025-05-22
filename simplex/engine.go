// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var _ common.Engine = (*Engine)(nil)

type Engine struct {
	common.Handler
	health.Checker

	log logging.Logger
}

func (e *Engine) Simplex(nodeID ids.NodeID, _ *p2p.Simplex) error {
	e.log.Debug("Simplex request from %s", zap.Stringer("nodeID", nodeID))
	return nil
}

func (*Engine) Start(_ context.Context, _ uint32) error {
	return nil
}
