// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package resource

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

type metrics struct {
	numCPUCycles       *prometheus.GaugeVec
	numDiskReads       *prometheus.GaugeVec
	numDiskReadBytes   *prometheus.GaugeVec
	numDiskWrites      *prometheus.GaugeVec
	numDiskWritesBytes *prometheus.GaugeVec
}

func newMetrics(registerer prometheus.Registerer) (*metrics, error) {
	m := &metrics{
		numCPUCycles: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "num_cpu_cycles",
				Help: "Total number of CPU cycles",
			},
			[]string{"processID"},
		),
		numDiskReads: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "num_disk_reads",
				Help: "Total number of disk reads",
			},
			[]string{"processID"},
		),
		numDiskReadBytes: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "num_disk_read_bytes",
				Help: "Total number of disk read bytes",
			},
			[]string{"processID"},
		),
		numDiskWrites: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "num_disk_writes",
				Help: "Total number of disk writes",
			},
			[]string{"processID"},
		),
		numDiskWritesBytes: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "num_disk_write_bytes",
				Help: "Total number of disk write bytes",
			},
			[]string{"processID"},
		),
	}
	err := errors.Join(
		registerer.Register(m.numCPUCycles),
		registerer.Register(m.numDiskReads),
		registerer.Register(m.numDiskReadBytes),
		registerer.Register(m.numDiskWrites),
		registerer.Register(m.numDiskWritesBytes),
	)
	return m, err
}
