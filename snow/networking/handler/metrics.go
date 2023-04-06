// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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
	expired      prometheus.Counter
	asyncExpired prometheus.Counter
	messages     map[message.Op]*messageProcessing
}

type messageProcessing struct {
	handlingTime    metric.Averager
	processingTime  metric.Averager
	acquireLockTime metric.Averager
}

func newMetrics(namespace string, reg prometheus.Registerer) (*metrics, error) {
	errs := wrappers.Errs{}

	expired := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "expired",
		Help:      "Incoming sync messages dropped because the message deadline expired",
	})
	asyncExpired := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "async_expired",
		Help:      "Incoming async messages dropped because the message deadline expired",
	})
	errs.Add(
		reg.Register(expired),
		reg.Register(asyncExpired),
	)

	messages := make(map[message.Op]*messageProcessing, len(message.ConsensusOps))
	for _, op := range message.ConsensusOps {
		opStr := op.String()
		messageProcessing := &messageProcessing{
			handlingTime: metric.NewAveragerWithErrs(
				namespace,
				opStr,
				fmt.Sprintf("time (in ns) of handling a %s", opStr),
				reg,
				&errs,
			),
			processingTime: metric.NewAveragerWithErrs(
				namespace,
				fmt.Sprintf("%s_processing", opStr),
				fmt.Sprintf("time (in ns) of processing a %s", opStr),
				reg,
				&errs,
			),
			acquireLockTime: metric.NewAveragerWithErrs(
				namespace,
				fmt.Sprintf("%s_lock", opStr),
				fmt.Sprintf("time (in ns) of acquiring a lock to process a %s", opStr),
				reg,
				&errs,
			),
		}
		messages[op] = messageProcessing
	}

	return &metrics{
		expired:      expired,
		asyncExpired: asyncExpired,
		messages:     messages,
	}, errs.Err
}
