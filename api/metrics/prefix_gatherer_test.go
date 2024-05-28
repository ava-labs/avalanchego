// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	dto "github.com/prometheus/client_model/go"
)

func TestPrefixGatherer_Gather(t *testing.T) {
	require := require.New(t)

	gatherer := NewPrefixGatherer()
	require.NotNil(gatherer)

	registerA := prometheus.NewRegistry()
	require.NoError(gatherer.Register("a", registerA))

	registerB := prometheus.NewRegistry()
	require.NoError(gatherer.Register("b", registerB))

	counterA := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "counter",
		Help: "help",
	})
	require.NoError(registerA.Register(counterA))

	counterB := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "counter",
		Help: "help",
	})
	counterB.Inc()
	require.NoError(registerB.Register(counterB))

	metrics, err := gatherer.Gather()
	require.NoError(err)
	require.Equal(
		[]*dto.MetricFamily{
			{
				Name: proto.String("a_counter"),
				Help: proto.String("help"),
				Type: dto.MetricType_COUNTER.Enum(),
				Metric: []*dto.Metric{
					{
						Label: []*dto.LabelPair{},
						Counter: &dto.Counter{
							Value: proto.Float64(0),
						},
					},
				},
			},
			{
				Name: proto.String("b_counter"),
				Help: proto.String("help"),
				Type: dto.MetricType_COUNTER.Enum(),
				Metric: []*dto.Metric{
					{
						Label: []*dto.LabelPair{},
						Counter: &dto.Counter{
							Value: proto.Float64(1),
						},
					},
				},
			},
		},
		metrics,
	)
}

func TestPrefixGatherer_Register(t *testing.T) {
	tests := []struct {
		name                   string
		prefixGatherer         *prefixGatherer
		prefix                 string
		gatherer               prometheus.Gatherer
		expectedErr            error
		expectedPrefixGatherer *prefixGatherer
	}{
		{
			name:           "first registration",
			prefixGatherer: &prefixGatherer{},
			prefix:         "first",
			gatherer:       &testGatherer{},
			expectedErr:    nil,
			expectedPrefixGatherer: &prefixGatherer{
				multiGatherer: multiGatherer{
					names: []string{"first"},
					gatherers: prometheus.Gatherers{
						&prefixedGatherer{
							prefix:   "first",
							gatherer: &testGatherer{},
						},
					},
				},
			},
		},
		{
			name: "second registration",
			prefixGatherer: &prefixGatherer{
				multiGatherer: multiGatherer{
					names: []string{"first"},
					gatherers: prometheus.Gatherers{
						&prefixedGatherer{
							prefix:   "first",
							gatherer: &testGatherer{},
						},
					},
				},
			},
			prefix: "second",
			gatherer: &testGatherer{
				mfs: []*dto.MetricFamily{{}},
			},
			expectedErr: nil,
			expectedPrefixGatherer: &prefixGatherer{
				multiGatherer: multiGatherer{
					names: []string{"first", "second"},
					gatherers: prometheus.Gatherers{
						&prefixedGatherer{
							prefix:   "first",
							gatherer: &testGatherer{},
						},
						&prefixedGatherer{
							prefix: "second",
							gatherer: &testGatherer{
								mfs: []*dto.MetricFamily{{}},
							},
						},
					},
				},
			},
		},
		{
			name: "conflicts with previous registration",
			prefixGatherer: &prefixGatherer{
				multiGatherer: multiGatherer{
					names: []string{"first"},
					gatherers: prometheus.Gatherers{
						&prefixedGatherer{
							prefix:   "first",
							gatherer: &testGatherer{},
						},
					},
				},
			},
			prefix: "first",
			gatherer: &testGatherer{
				mfs: []*dto.MetricFamily{{}},
			},
			expectedErr: errOverlappingNamespaces,
			expectedPrefixGatherer: &prefixGatherer{
				multiGatherer: multiGatherer{
					names: []string{"first"},
					gatherers: prometheus.Gatherers{
						&prefixedGatherer{
							prefix:   "first",
							gatherer: &testGatherer{},
						},
					},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			err := test.prefixGatherer.Register(test.prefix, test.gatherer)
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.expectedPrefixGatherer, test.prefixGatherer)
		})
	}
}

func TestEitherIsPrefix(t *testing.T) {
	tests := []struct {
		name     string
		a        string
		b        string
		expected bool
	}{
		{
			name:     "empty strings",
			a:        "",
			b:        "",
			expected: true,
		},
		{
			name:     "an empty string",
			a:        "",
			b:        "hello",
			expected: true,
		},
		{
			name:     "same strings",
			a:        "x",
			b:        "x",
			expected: true,
		},
		{
			name:     "different strings",
			a:        "x",
			b:        "y",
			expected: false,
		},
		{
			name:     "splits namespace",
			a:        "hello",
			b:        "hello_world",
			expected: true,
		},
		{
			name:     "is prefix before separator",
			a:        "hello",
			b:        "helloworld",
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			require.Equal(test.expected, eitherIsPrefix(test.a, test.b))
			require.Equal(test.expected, eitherIsPrefix(test.b, test.a))
		})
	}
}
