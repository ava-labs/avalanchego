// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/utils/metric"

	dto "github.com/prometheus/client_model/go"
)

var (
	_ MultiGatherer = (*prefixGatherer)(nil)

	errOverlappingNamespaces = errors.New("prefix could create overlapping namespaces")
)

// NewPrefixGatherer returns a new MultiGatherer that merges metrics by adding a
// prefix to their names.
func NewPrefixGatherer() MultiGatherer {
	return &prefixGatherer{}
}

type prefixGatherer struct {
	multiGatherer
}

func (g *prefixGatherer) Register(prefix string, gatherer prometheus.Gatherer) error {
	g.lock.Lock()
	defer g.lock.Unlock()

	for _, existingPrefix := range g.names {
		if eitherIsPrefix(prefix, existingPrefix) {
			return fmt.Errorf("%w: %q conflicts with %q",
				errOverlappingNamespaces,
				prefix,
				existingPrefix,
			)
		}
	}

	g.register(
		prefix,
		&prefixedGatherer{
			prefix:   prefix,
			gatherer: gatherer,
		},
	)
	return nil
}

type prefixedGatherer struct {
	prefix   string
	gatherer prometheus.Gatherer
}

func (g *prefixedGatherer) Gather() ([]*dto.MetricFamily, error) {
	// Gather returns partially filled metrics in the case of an error. So, it
	// is expected to still return the metrics in the case an error is returned.
	metricFamilies, err := g.gatherer.Gather()
	for _, metricFamily := range metricFamilies {
		metricFamily.Name = proto.String(metric.AppendNamespace(
			g.prefix,
			metricFamily.GetName(),
		))
	}
	return metricFamilies, err
}

// eitherIsPrefix returns true if either [a] is a prefix of [b] or [b] is a
// prefix of [a].
//
// This function accounts for the usage of the namespace boundary, so "hello" is
// not considered a prefix of "helloworld". However, "hello" is considered a
// prefix of "hello_world".
func eitherIsPrefix(a, b string) bool {
	if len(a) > len(b) {
		a, b = b, a
	}
	return a == b[:len(a)] && // a is a prefix of b
		(len(a) == 0 || // a is empty
			len(a) == len(b) || // a is equal to b
			b[len(a)] == metric.NamespaceSeparatorByte) // a ends at a namespace boundary of b
}
