// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"context"
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/snow/engine/common/queue"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var errMissingVtxDependenciesOnAccept = errors.New("attempting to execute blocked vertex")

type vtxParser struct {
	log                     logging.Logger
	numAccepted, numDropped prometheus.Counter
	manager                 vertex.Manager
}

func (p *vtxParser) Parse(ctx context.Context, vtxBytes []byte) (queue.Job, error) {
	vtx, err := p.manager.ParseVtx(ctx, vtxBytes)
	if err != nil {
		return nil, err
	}
	return &vertexJob{
		log:         p.log,
		numAccepted: p.numAccepted,
		numDropped:  p.numDropped,
		vtx:         vtx,
	}, nil
}

type vertexJob struct {
	log                     logging.Logger
	numAccepted, numDropped prometheus.Counter
	vtx                     avalanche.Vertex
}

func (v *vertexJob) ID() ids.ID {
	return v.vtx.ID()
}

func (v *vertexJob) MissingDependencies(context.Context) (ids.Set, error) {
	missing := ids.Set{}
	parents, err := v.vtx.Parents()
	if err != nil {
		return missing, err
	}
	for _, parent := range parents {
		if parent.Status() != choices.Accepted {
			missing.Add(parent.ID())
		}
	}
	return missing, nil
}

// Returns true if this vertex job has at least 1 missing dependency
func (v *vertexJob) HasMissingDependencies(context.Context) (bool, error) {
	parents, err := v.vtx.Parents()
	if err != nil {
		return false, err
	}
	for _, parent := range parents {
		if parent.Status() != choices.Accepted {
			return true, nil
		}
	}
	return false, nil
}

func (v *vertexJob) Execute(ctx context.Context) error {
	hasMissingDependencies, err := v.HasMissingDependencies(ctx)
	if err != nil {
		return err
	}
	if hasMissingDependencies {
		v.numDropped.Inc()
		return errMissingVtxDependenciesOnAccept
	}
	txs, err := v.vtx.Txs(ctx)
	if err != nil {
		return err
	}
	for _, tx := range txs {
		if tx.Status() != choices.Accepted {
			v.numDropped.Inc()
			v.log.Warn("attempting to execute vertex with non-accepted transactions")
			return nil
		}
	}
	status := v.vtx.Status()
	switch status {
	case choices.Unknown, choices.Rejected:
		v.numDropped.Inc()
		return fmt.Errorf("attempting to execute vertex with status %s", status)
	case choices.Processing:
		v.numAccepted.Inc()
		v.log.Trace("accepting vertex in bootstrapping",
			zap.Stringer("vtxID", v.vtx.ID()),
		)
		if err := v.vtx.Accept(ctx); err != nil {
			return fmt.Errorf("failed to accept vertex in bootstrapping: %w", err)
		}
	}
	return nil
}

func (v *vertexJob) Bytes() []byte {
	return v.vtx.Bytes()
}
