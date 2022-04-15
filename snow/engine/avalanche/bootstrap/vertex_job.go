// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/snow/choices"
	"github.com/chain4travel/caminogo/snow/consensus/avalanche"
	"github.com/chain4travel/caminogo/snow/engine/avalanche/vertex"
	"github.com/chain4travel/caminogo/snow/engine/common/queue"
	"github.com/chain4travel/caminogo/utils/logging"
)

var errMissingVtxDependenciesOnAccept = errors.New("attempting to execute blocked vertex")

type vtxParser struct {
	log                     logging.Logger
	numAccepted, numDropped prometheus.Counter
	manager                 vertex.Manager
}

func (p *vtxParser) Parse(vtxBytes []byte) (queue.Job, error) {
	vtx, err := p.manager.ParseVtx(vtxBytes)
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

func (v *vertexJob) ID() ids.ID { return v.vtx.ID() }

func (v *vertexJob) MissingDependencies() (ids.Set, error) {
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
func (v *vertexJob) HasMissingDependencies() (bool, error) {
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

func (v *vertexJob) Execute() error {
	hasMissingDependencies, err := v.HasMissingDependencies()
	if err != nil {
		return err
	}
	if hasMissingDependencies {
		v.numDropped.Inc()
		return errMissingVtxDependenciesOnAccept
	}
	txs, err := v.vtx.Txs()
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
		v.log.Trace("accepting vertex %s in bootstrapping", v.vtx.ID())
		if err := v.vtx.Accept(); err != nil {
			return fmt.Errorf("failed to accept vertex in bootstrapping: %w", err)
		}
	}
	return nil
}

func (v *vertexJob) Bytes() []byte { return v.vtx.Bytes() }
