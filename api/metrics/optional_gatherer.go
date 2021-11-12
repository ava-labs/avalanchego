// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"errors"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	dto "github.com/prometheus/client_model/go"
)

var (
	errDuplicatedRegister = errors.New("duplicated register")

	_ OptionalGatherer = &optionalGatherer{}
)

type OptionalGatherer interface {
	prometheus.Gatherer

	Register(gatherer prometheus.Gatherer) error
}

type optionalGatherer struct {
	lock     sync.RWMutex
	gatherer prometheus.Gatherer
}

func NewOptionalGatherer() OptionalGatherer {
	return &optionalGatherer{}
}

func (g *optionalGatherer) Gather() ([]*dto.MetricFamily, error) {
	g.lock.RLock()
	defer g.lock.RUnlock()

	if g.gatherer == nil {
		return nil, nil
	}
	return g.gatherer.Gather()
}

func (g *optionalGatherer) Register(gatherer prometheus.Gatherer) error {
	g.lock.Lock()
	defer g.lock.Unlock()

	if g.gatherer != nil {
		return errDuplicatedRegister
	}
	g.gatherer = gatherer
	return nil
}
