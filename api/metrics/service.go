// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/ava-labs/avalanchego/snow/engine/common"
)

// NewService returns a new prometheus service
func NewService() (*prometheus.Registry, *common.HTTPHandler) {
	registerer := prometheus.NewRegistry()
	handler := promhttp.InstrumentMetricHandler(
		registerer,
		promhttp.HandlerFor(
			registerer,
			promhttp.HandlerOpts{},
		),
	)
	return registerer, &common.HTTPHandler{LockOptions: common.NoLock, Handler: handler}
}
