// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type handlerMetrics struct {
	expired  prometheus.Counter
	messages map[message.Op]metric.Averager
	shutdown metric.Averager
}

// Initialize implements the Engine interface
func (m *handlerMetrics) Initialize(namespace string, reg prometheus.Registerer) error {
	m.expired = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "expired",
		Help:      "Incoming messages dropped because the message deadline expired",
	})

	errs := wrappers.Errs{}
	errs.Add(reg.Register(m.expired))

	m.messages = make(map[message.Op]metric.Averager, len(message.ConsensusOps))
	for _, op := range message.ConsensusOps {
		opStr := op.String()
		m.messages[op] = metric.NewAveragerWithErrs(
			namespace,
			opStr,
			fmt.Sprintf("time (in ns) of processing a %s", opStr),
			reg,
			&errs,
		)
	}

	m.shutdown = metric.NewAveragerWithErrs(
		namespace,
		"shutdown",
		"time (in ns) spent in the process of shutting down",
		reg,
		&errs,
	)
	return errs.Err
}
