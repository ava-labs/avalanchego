// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/chain4travel/caminogo/message"
	"github.com/chain4travel/caminogo/utils/metric"
	"github.com/chain4travel/caminogo/utils/wrappers"
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
