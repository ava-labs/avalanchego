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

func TestLabelGatherer_Gather(t *testing.T) {
	require := require.New(t)

	gatherer := NewLabelGatherer("smith")
	require.NotNil(gatherer)

	registerA := prometheus.NewRegistry()
	require.NoError(gatherer.Register("rick", registerA))

	registerB := prometheus.NewRegistry()
	require.NoError(gatherer.Register("morty", registerB))

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
				Name: proto.String("counter"),
				Help: proto.String("help"),
				Type: dto.MetricType_COUNTER.Enum(),
				Metric: []*dto.Metric{
					{
						Label: []*dto.LabelPair{
							{
								Name:  proto.String("smith"),
								Value: proto.String("morty"),
							},
						},
						Counter: &dto.Counter{
							Value: proto.Float64(1),
						},
					},
					{
						Label: []*dto.LabelPair{
							{
								Name:  proto.String("smith"),
								Value: proto.String("rick"),
							},
						},
						Counter: &dto.Counter{
							Value: proto.Float64(0),
						},
					},
				},
			},
		},
		metrics,
	)
}

func TestLabelGatherer_Register(t *testing.T) {
	tests := []struct {
		name                  string
		labelGatherer         *labelGatherer
		labelValue            string
		gatherer              prometheus.Gatherer
		expectedErr           error
		expectedLabelGatherer *labelGatherer
	}{
		{
			name:          "first registration",
			labelGatherer: &labelGatherer{},
			labelValue:    "first",
			gatherer:      &testGatherer{},
			expectedErr:   nil,
			expectedLabelGatherer: &labelGatherer{
				multiGatherer: multiGatherer{
					names: []string{"first"},
					gatherers: prometheus.Gatherers{
						&labeledGatherer{
							labelValue: "first",
							gatherer:   &testGatherer{},
						},
					},
				},
			},
		},
		{
			name: "second registration",
			labelGatherer: &labelGatherer{
				multiGatherer: multiGatherer{
					names: []string{"first"},
					gatherers: prometheus.Gatherers{
						&labeledGatherer{
							labelValue: "first",
							gatherer:   &testGatherer{},
						},
					},
				},
			},
			labelValue: "second",
			gatherer: &testGatherer{
				mfs: []*dto.MetricFamily{{}},
			},
			expectedErr: nil,
			expectedLabelGatherer: &labelGatherer{
				multiGatherer: multiGatherer{
					names: []string{"first", "second"},
					gatherers: prometheus.Gatherers{
						&labeledGatherer{
							labelValue: "first",
							gatherer:   &testGatherer{},
						},
						&labeledGatherer{
							labelValue: "second",
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
			labelGatherer: &labelGatherer{
				multiGatherer: multiGatherer{
					names: []string{"first"},
					gatherers: prometheus.Gatherers{
						&labeledGatherer{
							labelValue: "first",
							gatherer:   &testGatherer{},
						},
					},
				},
			},
			labelValue: "first",
			gatherer: &testGatherer{
				mfs: []*dto.MetricFamily{{}},
			},
			expectedErr: errDuplicateGatherer,
			expectedLabelGatherer: &labelGatherer{
				multiGatherer: multiGatherer{
					names: []string{"first"},
					gatherers: prometheus.Gatherers{
						&labeledGatherer{
							labelValue: "first",
							gatherer:   &testGatherer{},
						},
					},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			err := test.labelGatherer.Register(test.labelValue, test.gatherer)
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.expectedLabelGatherer, test.labelGatherer)
		})
	}
}
