// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"fmt"
	"slices"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils"

	dto "github.com/prometheus/client_model/go"
)

// MultiGatherer extends the Gatherer interface by allowing additional gatherers
// to be registered.
type MultiGatherer interface {
	prometheus.Gatherer

	// Register adds the outputs of [gatherer] to the results of future calls to
	// Gather with the provided [name] added to the metrics.
	Register(name string, gatherer prometheus.Gatherer) error

	// Deregister removes the outputs of a gatherer with [name] from the results
	// of future calls to Gather. Returns true if a gatherer with [name] was
	// found.
	Deregister(name string) bool
}

type multiGatherer struct {
	lock      sync.RWMutex
	names     []string
	gatherers prometheus.Gatherers
}

func (g *multiGatherer) Gather() ([]*dto.MetricFamily, error) {
	g.lock.RLock()
	defer g.lock.RUnlock()

	return g.gatherers.Gather()
}

func (g *multiGatherer) register(name string, gatherer prometheus.Gatherer) {
	g.names = append(g.names, name)
	g.gatherers = append(g.gatherers, gatherer)
}

func (g *multiGatherer) Deregister(name string) bool {
	g.lock.Lock()
	defer g.lock.Unlock()

	index := slices.Index(g.names, name)
	if index == -1 {
		return false
	}

	g.names = utils.DeleteIndex(g.names, index)
	g.gatherers = utils.DeleteIndex(g.gatherers, index)
	return true
}

func MakeAndRegister(gatherer MultiGatherer, name string) (*prometheus.Registry, error) {
	reg := prometheus.NewRegistry()
	if err := gatherer.Register(name, reg); err != nil {
		return nil, fmt.Errorf("couldn't register %q metrics: %w", name, err)
	}
	return reg, nil
}
