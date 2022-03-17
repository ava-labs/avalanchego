// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type MessageMetrics struct {
	ReceivedBytes, SentBytes, NumSent, NumFailed, NumReceived prometheus.Counter
	SavedReceivedBytes, SavedSentBytes                        metric.Averager
}

func NewMessageMetrics(
	op message.Op,
	namespace string,
	metrics prometheus.Registerer,
	errs *wrappers.Errs,
) *MessageMetrics {
	msg := &MessageMetrics{
		NumSent: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      fmt.Sprintf("%s_sent", op),
			Help:      fmt.Sprintf("Number of %s messages sent over the network", op),
		}),
		NumFailed: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      fmt.Sprintf("%s_failed", op),
			Help:      fmt.Sprintf("Number of %s messages that failed to be sent over the network", op),
		}),
		NumReceived: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      fmt.Sprintf("%s_received", op),
			Help:      fmt.Sprintf("Number of %s messages received from the network", op),
		}),
		ReceivedBytes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      fmt.Sprintf("%s_received_bytes", op),
			Help:      fmt.Sprintf("Number of bytes of %s messages received from the network", op),
		}),
		SentBytes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      fmt.Sprintf("%s_sent_bytes", op),
			Help:      fmt.Sprintf("Size of bytes of %s messages received from the network", op),
		}),
	}
	errs.Add(
		metrics.Register(msg.NumSent),
		metrics.Register(msg.NumFailed),
		metrics.Register(msg.NumReceived),
		metrics.Register(msg.ReceivedBytes),
		metrics.Register(msg.SentBytes),
	)

	if op.Compressible() {
		msg.SavedReceivedBytes = metric.NewAveragerWithErrs(
			namespace,
			fmt.Sprintf("%s_compression_saved_received_bytes", op),
			fmt.Sprintf("bytes saved (not received) due to compression of %s messages", op),
			metrics,
			errs,
		)
		msg.SavedSentBytes = metric.NewAveragerWithErrs(
			namespace,
			fmt.Sprintf("%s_compression_saved_sent_bytes", op),
			fmt.Sprintf("bytes saved (not sent) due to compression of %s messages", op),
			metrics,
			errs,
		)
	} else {
		msg.SavedReceivedBytes = metric.NewNoAverager()
		msg.SavedSentBytes = metric.NewNoAverager()
	}
	return msg
}

type Metrics struct {
	Log            logging.Logger
	FailedToParse  prometheus.Counter
	MessageMetrics map[message.Op]*MessageMetrics
}

func NewMetrics(
	log logging.Logger,
	namespace string,
	registerer prometheus.Registerer,
) (*Metrics, error) {
	m := &Metrics{
		Log: log,
		FailedToParse: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "msgs_failed_to_parse",
			Help:      "Number of messages that could not be parsed or were invalidly formed",
		}),
		MessageMetrics: make(map[message.Op]*MessageMetrics, len(message.ExternalOps)),
	}

	errs := wrappers.Errs{}
	errs.Add(registerer.Register(m.FailedToParse))
	for _, op := range message.ExternalOps {
		m.MessageMetrics[op] = NewMessageMetrics(op, namespace, registerer, &errs)
	}
	return m, errs.Err
}

// Sent updates the metrics for having sent [msg] and removes a reference from
// the [msg].
func (m *Metrics) Sent(msg message.OutboundMessage) {
	op := msg.Op()
	msgMetrics := m.MessageMetrics[op]
	if msgMetrics == nil {
		m.Log.Error(
			"unknown message being sent with op %s",
			op,
		)
		msg.DecRef()
		return
	}
	msgMetrics.NumSent.Inc()
	msgMetrics.SentBytes.Add(float64(len(msg.Bytes())))
	// assume that if [saved] == 0, [msg] wasn't compressed
	if saved := msg.BytesSavedCompression(); saved != 0 {
		msgMetrics.SavedSentBytes.Observe(float64(saved))
	}
	msg.DecRef()
}

func (m *Metrics) MultipleSendsFailed(op message.Op, count int) {
	msgMetrics := m.MessageMetrics[op]
	if msgMetrics == nil {
		m.Log.Error(
			"%d unknown messages failed to be sent with op %s",
			count,
			op,
		)
		return
	}
	msgMetrics.NumFailed.Add(float64(count))
}

// SendFailed updates the metrics for having failed to send [msg] and removes a
// reference from the [msg].
func (m *Metrics) SendFailed(msg message.OutboundMessage) {
	op := msg.Op()
	msgMetrics := m.MessageMetrics[op]
	if msgMetrics == nil {
		m.Log.Error(
			"unknown message failed to be sent with op %s",
			op,
		)
		msg.DecRef()
		return
	}
	msgMetrics.NumFailed.Inc()
	msg.DecRef()
}

func (m *Metrics) Received(msg message.InboundMessage, msgLen uint32) {
	op := msg.Op()
	msgMetrics := m.MessageMetrics[op]
	if msgMetrics == nil {
		m.Log.Error(
			"unknown message received with op %s",
			op,
		)
		return
	}
	msgMetrics.NumReceived.Inc()
	msgMetrics.ReceivedBytes.Add(float64(msgLen))
	// assume that if [saved] == 0, [msg] wasn't compressed
	if saved := msg.BytesSavedCompression(); saved != 0 {
		msgMetrics.SavedReceivedBytes.Observe(float64(saved))
	}
}
