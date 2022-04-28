// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type metrics struct {
	expired  prometheus.Counter
	messages map[message.Op]metric.Averager
}

func newMetrics(namespace string, reg prometheus.Registerer) (*metrics, error) {
	errs := wrappers.Errs{}

	expired := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "expired",
		Help:      "Incoming messages dropped because the message deadline expired",
	})
	errs.Add(reg.Register(expired))

	messages := make(map[message.Op]metric.Averager, len(message.ConsensusOps))
	for _, op := range message.ConsensusOps {
		opStr := op.String()
		messages[op] = metric.NewAveragerWithErrs(
			namespace,
			opStr,
			fmt.Sprintf("time (in ns) of processing a %s", opStr),
			reg,
			&errs,
		)
	}

	return &metrics{
		expired:  expired,
		messages: messages,
	}, errs.Err
}
