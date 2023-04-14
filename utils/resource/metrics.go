// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package resource

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type metrics struct {
	numCPUCycles     *prometheus.GaugeVec
	numDiskReads     *prometheus.GaugeVec
	numDiskReadBytes *prometheus.GaugeVec

	numDiskWrites      *prometheus.GaugeVec
	numDiskWritesBytes *prometheus.GaugeVec
}

func newMetrics(namespace string, registerer prometheus.Registerer, processes map[int]*proc) (*metrics, error) {
	m := &metrics{
		numCPUCycles: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "Total number of CPU cycles",
				Help:      "Total number of CPU cycles",
			},
			[]string{"processID"},
		),
		numDiskReads: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "Total number of disk reads",
				Help:      "Total number of disk reads",
			},
			[]string{"processID"},
		),
		numDiskReadBytes: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "Total number of disk read bytes",
				Help:      "Total number of disk read bytes",
			},
			[]string{"processID"},
		),
		numDiskWrites: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "Total number of disk writes",
				Help:      "Total number of disk writes",
			},
			[]string{"processID"},
		),
		numDiskWritesBytes: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "Total number of disk write bytes",
			Help:      "Total number of disk write bytes",
		},
			[]string{"processID"},
		),
	}
	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(m.numCPUCycles),
		registerer.Register(m.numDiskReads),
		registerer.Register(m.numDiskReadBytes),
		registerer.Register(m.numDiskWrites),
		registerer.Register(m.numDiskWritesBytes),
	)

	for processID := range processes {
		// initialize to 0
		processIDStr := strconv.Itoa(processID)
		m.numCPUCycles.WithLabelValues(processIDStr).Set(0)
		m.numDiskReads.WithLabelValues(processIDStr).Set(0)
		m.numDiskReadBytes.WithLabelValues(processIDStr).Set(0)
		m.numDiskWrites.WithLabelValues(processIDStr).Set(0)
		m.numDiskWritesBytes.WithLabelValues(processIDStr).Set(0)
	}
	return m, errs.Err
}
