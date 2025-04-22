// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chainindex

import "github.com/prometheus/client_golang/prometheus"

type metrics struct {
	deletedBlocks prometheus.Counter
}

func newMetrics(registry prometheus.Registerer) (*metrics, error) {
	m := &metrics{
		deletedBlocks: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "chainindex",
			Name:      "deleted_blocks",
			Help:      "Number of blocks deleted from the chain",
		}),
	}

	if err := registry.Register(m.deletedBlocks); err != nil {
		return nil, err
	}

	return m, nil
}
