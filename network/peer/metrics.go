// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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

	// The per-message paths below run for every message sent to and received
	// from every peer, so the children of the vectors above are resolved once
	// here rather than looked up by label set on every call.
	sent          ioMetrics
	received      ioMetrics
	numSendFailed [message.NumOps]prometheus.Counter
}

// ioMetrics holds the children of the io-labeled vectors for a single value of
// the io label, indexed by op.
type ioMetrics struct {
	io            string
	messagesVec   *prometheus.CounterVec
	bytesVec      *prometheus.CounterVec
	bytesSavedVec *prometheus.GaugeVec

	// messages is indexed by op, then by whether the message was compressed.
	messages   [message.NumOps][2]prometheus.Counter
	bytes      [message.NumOps]prometheus.Counter
	bytesSaved [message.NumOps]prometheus.Gauge
}

func newIOMetrics(
	io string,
	messages *prometheus.CounterVec,
	bytes *prometheus.CounterVec,
	bytesSaved *prometheus.GaugeVec,
) ioMetrics {
	m := ioMetrics{
		io:            io,
		messagesVec:   messages,
		bytesVec:      bytes,
		bytesSavedVec: bytesSaved,
	}
	for op := range message.NumOps {
		opStr := op.String()
		m.messages[op] = [2]prometheus.Counter{
			messages.WithLabelValues(io, opStr, "false"),
			messages.WithLabelValues(io, opStr, "true"),
		}
		m.bytes[op] = bytes.WithLabelValues(io, opStr)
		m.bytesSaved[op] = bytesSaved.WithLabelValues(io, opStr)
	}
	return m
}

// observe records a message of numBytes bytes that saved bytesSaved bytes by
// being compressed. A message that saved no bytes is assumed to have been sent
// uncompressed.
func (m *ioMetrics) observe(op message.Op, numBytes int, bytesSaved int) {
	compressed := bytesSaved != 0
	if op >= message.NumOps {
		// Only defined ops are expected here, but an undefined op MUST NOT
		// panic the node, so resolve its labels the slow way.
		opStr := op.String()
		m.messagesVec.WithLabelValues(m.io, opStr, strconv.FormatBool(compressed)).Inc()
		m.bytesVec.WithLabelValues(m.io, opStr).Add(float64(numBytes))
		m.bytesSavedVec.WithLabelValues(m.io, opStr).Add(float64(bytesSaved))
		return
	}

	compressedIndex := 0
	if compressed {
		compressedIndex = 1
	}
	m.messages[op][compressedIndex].Inc()
	m.bytes[op].Add(float64(numBytes))
	m.bytesSaved[op].Add(float64(bytesSaved))
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
	m.sent = newIOMetrics(sentLabel, m.Messages, m.Bytes, m.BytesSaved)
	m.received = newIOMetrics(receivedLabel, m.Messages, m.Bytes, m.BytesSaved)
	for op := range message.NumOps {
		m.numSendFailed[op] = m.NumSendFailed.WithLabelValues(op.String())
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
	m.sent.observe(msg.Op, len(msg.Bytes), msg.BytesSavedCompression)
}

func (m *Metrics) MultipleSendsFailed(op message.Op, count int) {
	m.sendFailed(op).Add(float64(count))
}

// SendFailed updates the metrics for having failed to send [msg].
func (m *Metrics) SendFailed(msg *message.OutboundMessage) {
	m.sendFailed(msg.Op).Inc()
}

func (m *Metrics) sendFailed(op message.Op) prometheus.Counter {
	if op >= message.NumOps {
		// See [ioMetrics.observe].
		return m.NumSendFailed.WithLabelValues(op.String())
	}
	return m.numSendFailed[op]
}

// Received updates the metrics for having received [msg].
func (m *Metrics) Received(msg *message.InboundMessage, msgLen uint32) {
	m.received.observe(msg.Op, int(msgLen), msg.BytesSavedCompression)
}
