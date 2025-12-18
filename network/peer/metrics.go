// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"errors"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/message"
)

const (
	ioLabel         = "io"
	opLabel         = "op"
	compressedLabel = "compressed"

	sentLabel     = "sent"
	receivedLabel = "received"
)

var (
	opLabels             = []string{opLabel}
	ioOpLabels           = []string{ioLabel, opLabel}
	ioOpCompressedLabels = []string{ioLabel, opLabel, compressedLabel}
)

type Metrics struct {
	ClockSkewCount prometheus.Counter
	ClockSkewSum   prometheus.Gauge

	RTTCount prometheus.Counter
	RTTSum   prometheus.Gauge

	NumFailedToParse prometheus.Counter
	NumSendFailed    *prometheus.CounterVec // op

	Messages   *prometheus.CounterVec // io + op + compressed
	Bytes      *prometheus.CounterVec // io + op
	BytesSaved *prometheus.GaugeVec   // io + op
}

func NewMetrics(registerer prometheus.Registerer) (*Metrics, error) {
	m := &Metrics{
		RTTCount: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "round_trip_count",
			Help: "number of RTT samples taken (n)",
		}),
		RTTSum: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "round_trip_sum",
			Help: "sum of RTT samples taken (ms)",
		}),
		ClockSkewCount: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "clock_skew_count",
			Help: "number of handshake timestamps inspected (n)",
		}),
		ClockSkewSum: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "clock_skew_sum",
			Help: "sum of (peer timestamp - local timestamp) from handshake messages (s)",
		}),
		NumFailedToParse: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "msgs_failed_to_parse",
			Help: "number of received messages that could not be parsed",
		}),
		NumSendFailed: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "msgs_failed_to_send",
				Help: "number of messages that failed to be sent",
			},
			opLabels,
		),
		Messages: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "msgs",
				Help: "number of handled messages",
			},
			ioOpCompressedLabels,
		),
		Bytes: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "msgs_bytes",
				Help: "number of message bytes",
			},
			ioOpLabels,
		),
		BytesSaved: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "msgs_bytes_saved",
				Help: "number of message bytes saved",
			},
			ioOpLabels,
		),
	}
	return m, errors.Join(
		registerer.Register(m.RTTCount),
		registerer.Register(m.RTTSum),
		registerer.Register(m.ClockSkewCount),
		registerer.Register(m.ClockSkewSum),
		registerer.Register(m.NumFailedToParse),
		registerer.Register(m.NumSendFailed),
		registerer.Register(m.Messages),
		registerer.Register(m.Bytes),
		registerer.Register(m.BytesSaved),
	)
}

// Sent updates the metrics for having sent [msg].
func (m *Metrics) Sent(msg *message.OutboundMessage) {
	op := msg.Op.String()
	saved := msg.BytesSavedCompression
	compressed := saved != 0 // assume that if [saved] == 0, [msg] wasn't compressed
	compressedStr := strconv.FormatBool(compressed)

	m.Messages.With(prometheus.Labels{
		ioLabel:         sentLabel,
		opLabel:         op,
		compressedLabel: compressedStr,
	}).Inc()

	bytesLabel := prometheus.Labels{
		ioLabel: sentLabel,
		opLabel: op,
	}
	m.Bytes.With(bytesLabel).Add(float64(len(msg.Bytes)))
	m.BytesSaved.With(bytesLabel).Add(float64(saved))
}

func (m *Metrics) MultipleSendsFailed(op message.Op, count int) {
	m.NumSendFailed.With(prometheus.Labels{
		opLabel: op.String(),
	}).Add(float64(count))
}

// SendFailed updates the metrics for having failed to send [msg].
func (m *Metrics) SendFailed(msg *message.OutboundMessage) {
	op := msg.Op.String()
	m.NumSendFailed.With(prometheus.Labels{
		opLabel: op,
	}).Inc()
}

func (m *Metrics) Received(msg *message.InboundMessage, msgLen uint32) {
	op := msg.Op.String()
	saved := msg.BytesSavedCompression
	compressed := saved != 0 // assume that if [saved] == 0, [msg] wasn't compressed
	compressedStr := strconv.FormatBool(compressed)

	m.Messages.With(prometheus.Labels{
		ioLabel:         receivedLabel,
		opLabel:         op,
		compressedLabel: compressedStr,
	}).Inc()

	bytesLabel := prometheus.Labels{
		ioLabel: receivedLabel,
		opLabel: op,
	}
	m.Bytes.With(bytesLabel).Add(float64(msgLen))
	m.BytesSaved.With(bytesLabel).Add(float64(saved))
}
