package gossip

import (
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	ioLabel    = "io"
	sentIO     = "sent"
	receivedIO = "received"

	typeLabel  = "type"
	pushType   = "push"
	pullType   = "pull"
	unsentType = "unsent"
	sentType   = "sent"

	// dropped indicate that the gossipable element was not added to the set due to some reason.
	// for sent message, we'll use notReceive below.
	droppedLabel = "dropped"

	droppedMalformed = "malformed"
	droppedDuplicate = "duplicate"
	droppedOther     = "other"
	droppedNot       = "not"
	notReceive       = "not_receive"
)

var (
	ioTypeDroppedLabels = []string{ioLabel, typeLabel, droppedLabel}
	sentPushLabels      = prometheus.Labels{
		ioLabel:      sentIO,
		typeLabel:    pushType,
		droppedLabel: notReceive,
	}
	sentPullLabels = prometheus.Labels{
		ioLabel:      sentIO,
		typeLabel:    pullType,
		droppedLabel: notReceive,
	}
	typeLabels   = []string{typeLabel}
	unsentLabels = prometheus.Labels{
		typeLabel: unsentType,
	}
	sentLabels = prometheus.Labels{
		typeLabel: sentType,
	}
)

// Metrics that are tracked across a gossip protocol. A given protocol should
// only use a single instance of Metrics.
type Metrics struct {
	count                   *prometheus.CounterVec
	bytes                   *prometheus.CounterVec
	tracking                *prometheus.GaugeVec
	trackingLifetimeAverage prometheus.Gauge
	topValidators           *prometheus.GaugeVec

	lock               sync.Mutex
	malformedCount     int
	malformedBytes     int
	gossipables        map[ids.ID]int
	labeledGossipables map[string][]labeledGossipable
}

type labeledGossipable struct {
	ids.ID
	size int
}

// NewMetrics returns a common set of metrics
func NewMetrics(
	metrics prometheus.Registerer,
	namespace string,
) (*Metrics, error) {
	m := &Metrics{
		count: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "gossip_count",
				Help:      "amount of gossip (n)",
			},
			ioTypeDroppedLabels,
		),
		bytes: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "gossip_bytes",
				Help:      "amount of gossip (bytes)",
			},
			ioTypeDroppedLabels,
		),
		tracking: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "gossip_tracking",
				Help:      "number of gossipables being tracked",
			},
			typeLabels,
		),
		trackingLifetimeAverage: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "gossip_tracking_lifetime_average",
			Help:      "average duration a gossipable has been tracked (ns)",
		}),
		topValidators: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "top_validators",
				Help:      "number of validators gossipables are sent to due to stake",
			},
			typeLabels,
		),
		gossipables:        make(map[ids.ID]int),
		labeledGossipables: make(map[string][]labeledGossipable),
	}
	err := errors.Join(
		metrics.Register(m.count),
		metrics.Register(m.bytes),
		metrics.Register(m.tracking),
		metrics.Register(m.trackingLifetimeAverage),
		metrics.Register(m.topValidators),
	)
	return m, err
}

func (m *Metrics) observeMessage(labels prometheus.Labels, count int, bytes int) error {
	countMetric, err := m.count.GetMetricWith(labels)
	if err != nil {
		return fmt.Errorf("failed to get count metric: %w", err)
	}

	bytesMetric, err := m.bytes.GetMetricWith(labels)
	if err != nil {
		return fmt.Errorf("failed to get bytes metric: %w", err)
	}

	countMetric.Add(float64(count))
	bytesMetric.Add(float64(bytes))
	return nil
}

func (m *Metrics) observeIncomingMalformedGossipable(txBytes int) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.malformedCount++
	m.malformedBytes += txBytes
}

func (m *Metrics) trackGossipable(txID ids.ID, gossipableBytes int) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.gossipables[txID] = gossipableBytes
}

func (m *Metrics) ObserveIncomingGossipable(txID ids.ID, label string) {
	m.lock.Lock()
	defer m.lock.Unlock()
	size, ok := m.gossipables[txID]
	if !ok {
		return
	}
	delete(m.gossipables, txID)
	m.labeledGossipables[label] = append(m.labeledGossipables[label], labeledGossipable{ID: txID, size: size})
}

func (m *Metrics) observeReceivedMessage(typeLabel string) error {
	m.lock.Lock()
	defer m.lock.Unlock()
	for label, gossipables := range m.labeledGossipables {
		prometheusLabel := prometheus.Labels{
			ioLabel:      receivedIO,
			typeLabel:    typeLabel,
			droppedLabel: label,
		}
		count := len(gossipables)
		gossipableSize := 0
		for _, gossipable := range gossipables {
			gossipableSize += gossipable.size
		}
		m.observeMessage(prometheusLabel, count, gossipableSize)
		delete(m.labeledGossipables, label)
	}
	// add another label for the malformed entries ( since these doesn't have gossip id )
	prometheusLabel := prometheus.Labels{
		ioLabel:      receivedIO,
		typeLabel:    typeLabel,
		droppedLabel: droppedMalformed,
	}
	m.observeMessage(prometheusLabel, m.malformedCount, m.malformedBytes)
	m.malformedCount = 0
	m.malformedBytes = 0
	return nil
}
