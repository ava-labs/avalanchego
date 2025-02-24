// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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
	{
		counterA := prometheus.NewCounter(counterOpts)
		require.NoError(registerA.Register(counterA))
	}

	registerB := prometheus.NewRegistry()
	require.NoError(gatherer.Register("b", registerB))
	{
		counterB := prometheus.NewCounter(counterOpts)
		counterB.Inc()
		require.NoError(registerB.Register(counterB))
	}

	metrics, err := gatherer.Gather()
	require.NoError(err)
	require.Equal(
		[]*dto.MetricFamily{
			{
				Name: proto.String("a_counter"),
				Help: proto.String(counterOpts.Help),
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
				Help: proto.String(counterOpts.Help),
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
	firstPrefixedGatherer := &prefixedGatherer{
		prefix:   "first",
		gatherer: &testGatherer{},
	}
	firstPrefixGatherer := func() *prefixGatherer {
		return &prefixGatherer{
			multiGatherer: multiGatherer{
				names: []string{
					firstPrefixedGatherer.prefix,
				},
				gatherers: prometheus.Gatherers{
					firstPrefixedGatherer,
				},
			},
		}
	}
	secondPrefixedGatherer := &prefixedGatherer{
		prefix: "second",
		gatherer: &testGatherer{
			mfs: []*dto.MetricFamily{{}},
		},
	}
	secondPrefixGatherer := &prefixGatherer{
		multiGatherer: multiGatherer{
			names: []string{
				firstPrefixedGatherer.prefix,
				secondPrefixedGatherer.prefix,
			},
			gatherers: prometheus.Gatherers{
				firstPrefixedGatherer,
				secondPrefixedGatherer,
			},
		},
	}

	tests := []struct {
		name                   string
		prefixGatherer         *prefixGatherer
		prefix                 string
		gatherer               prometheus.Gatherer
		expectedErr            error
		expectedPrefixGatherer *prefixGatherer
	}{
		{
			name:                   "first registration",
			prefixGatherer:         &prefixGatherer{},
			prefix:                 firstPrefixedGatherer.prefix,
			gatherer:               firstPrefixedGatherer.gatherer,
			expectedErr:            nil,
			expectedPrefixGatherer: firstPrefixGatherer(),
		},
		{
			name:                   "second registration",
			prefixGatherer:         firstPrefixGatherer(),
			prefix:                 secondPrefixedGatherer.prefix,
			gatherer:               secondPrefixedGatherer.gatherer,
			expectedErr:            nil,
			expectedPrefixGatherer: secondPrefixGatherer,
		},
		{
			name:                   "conflicts with previous registration",
			prefixGatherer:         firstPrefixGatherer(),
			prefix:                 firstPrefixedGatherer.prefix,
			gatherer:               secondPrefixedGatherer.gatherer,
			expectedErr:            errOverlappingNamespaces,
			expectedPrefixGatherer: firstPrefixGatherer(),
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
