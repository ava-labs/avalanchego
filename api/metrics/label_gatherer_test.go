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
	const (
		labelName         = "smith"
		labelValueA       = "rick"
		labelValueB       = "morty"
		customLabelName   = "tag"
		customLabelValueA = "a"
		customLabelValueB = "b"
	)
	tests := []struct {
		name            string
		labelName       string
		expectedMetrics []*dto.Metric
		expectErr       bool
	}{
		{
			name:      "no overlap",
			labelName: customLabelName,
			expectedMetrics: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{
							Name:  proto.String(labelName),
							Value: proto.String(labelValueB),
						},
						{
							Name:  proto.String(customLabelName),
							Value: proto.String(customLabelValueB),
						},
					},
					Counter: &dto.Counter{
						Value: proto.Float64(1),
					},
				},
				{
					Label: []*dto.LabelPair{
						{
							Name:  proto.String(labelName),
							Value: proto.String(labelValueA),
						},
						{
							Name:  proto.String(customLabelName),
							Value: proto.String(customLabelValueA),
						},
					},
					Counter: &dto.Counter{
						Value: proto.Float64(0),
					},
				},
			},
			expectErr: false,
		},
		{
			name:      "has overlap",
			labelName: labelName,
			expectedMetrics: []*dto.Metric{
				{
					Label: []*dto.LabelPair{
						{
							Name:  proto.String(labelName),
							Value: proto.String(labelValueB),
						},
						{
							Name:  proto.String(customLabelName),
							Value: proto.String(customLabelValueB),
						},
					},
					Counter: &dto.Counter{
						Value: proto.Float64(1),
					},
				},
			},
			expectErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			gatherer := NewLabelGatherer(labelName)
			require.NotNil(gatherer)

			registerA := prometheus.NewRegistry()
			require.NoError(gatherer.Register(labelValueA, registerA))
			{
				counterA := prometheus.NewCounterVec(
					counterOpts,
					[]string{test.labelName},
				)
				counterA.With(prometheus.Labels{test.labelName: customLabelValueA})
				require.NoError(registerA.Register(counterA))
			}

			registerB := prometheus.NewRegistry()
			require.NoError(gatherer.Register(labelValueB, registerB))
			{
				counterB := prometheus.NewCounterVec(
					counterOpts,
					[]string{customLabelName},
				)
				counterB.With(prometheus.Labels{customLabelName: customLabelValueB}).Inc()
				require.NoError(registerB.Register(counterB))
			}

			metrics, err := gatherer.Gather()
			if test.expectErr {
				require.Error(err) //nolint:forbidigo // the error is not exported
			} else {
				require.NoError(err)
			}
			require.Equal(
				[]*dto.MetricFamily{
					{
						Name:   proto.String(counterOpts.Name),
						Help:   proto.String(counterOpts.Help),
						Type:   dto.MetricType_COUNTER.Enum(),
						Metric: test.expectedMetrics,
					},
				},
				metrics,
			)
		})
	}
}

func TestLabelGatherer_Register(t *testing.T) {
	firstLabeledGatherer := &labeledGatherer{
		labelValue: "first",
		gatherer:   &testGatherer{},
	}
	firstLabelGatherer := func() *labelGatherer {
		return &labelGatherer{
			multiGatherer: multiGatherer{
				names: []string{firstLabeledGatherer.labelValue},
				gatherers: prometheus.Gatherers{
					firstLabeledGatherer,
				},
			},
		}
	}
	secondLabeledGatherer := &labeledGatherer{
		labelValue: "second",
		gatherer: &testGatherer{
			mfs: []*dto.MetricFamily{{}},
		},
	}
	secondLabelGatherer := &labelGatherer{
		multiGatherer: multiGatherer{
			names: []string{
				firstLabeledGatherer.labelValue,
				secondLabeledGatherer.labelValue,
			},
			gatherers: prometheus.Gatherers{
				firstLabeledGatherer,
				secondLabeledGatherer,
			},
		},
	}

	tests := []struct {
		name                  string
		labelGatherer         *labelGatherer
		labelValue            string
		gatherer              prometheus.Gatherer
		expectedErr           error
		expectedLabelGatherer *labelGatherer
	}{
		{
			name:                  "first registration",
			labelGatherer:         &labelGatherer{},
			labelValue:            "first",
			gatherer:              firstLabeledGatherer.gatherer,
			expectedErr:           nil,
			expectedLabelGatherer: firstLabelGatherer(),
		},
		{
			name:                  "second registration",
			labelGatherer:         firstLabelGatherer(),
			labelValue:            "second",
			gatherer:              secondLabeledGatherer.gatherer,
			expectedErr:           nil,
			expectedLabelGatherer: secondLabelGatherer,
		},
		{
			name:                  "conflicts with previous registration",
			labelGatherer:         firstLabelGatherer(),
			labelValue:            "first",
			gatherer:              secondLabeledGatherer.gatherer,
			expectedErr:           errDuplicateGatherer,
			expectedLabelGatherer: firstLabelGatherer(),
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
