// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chainindex

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

type metrics struct {
	indexedBlocks prometheus.Counter
	deletedBlocks prometheus.Counter
}

func newMetrics(registry prometheus.Registerer) (*metrics, error) {
	m := &metrics{
		indexedBlocks: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "chainindex",
			Name:      "num_indexed_blocks",
			Help:      "Number of blocks indexed",
		}),
		deletedBlocks: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "chainindex",
			Name:      "deleted_blocks",
			Help:      "Number of blocks deleted from the chain",
		}),
	}

	if err := errors.Join(
		registry.Register(m.indexedBlocks),
		registry.Register(m.deletedBlocks),
	); err != nil {
		return nil, err
	}

	return m, nil
}
