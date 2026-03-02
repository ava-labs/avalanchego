// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

func describeCollector(collector prometheus.Collector) string {
	descs := make(chan *prometheus.Desc, 16)
	go func() {
		collector.Describe(descs)
		close(descs)
	}()

	parts := make([]string, 0, 4)
	for desc := range descs {
		parts = append(parts, desc.String())
	}
	return strings.Join(parts, ", ")
}
