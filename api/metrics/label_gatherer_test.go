// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

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
		name        string
		labelName   string
		wantMetrics string
		expectErr   bool
	}{
		{
			name:      "no overlap",
			labelName: customLabelName,
			wantMetrics: `
# HELP counter help
# TYPE counter counter
counter{smith="morty",tag="b"} 1
counter{smith="rick",tag="a"} 0
`,
		},
		{
			name:      "has overlap",
			labelName: labelName,
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

			if test.expectErr {
				require.Error(testutil.GatherAndCompare( //nolint:forbidigo // the error is not exported
					gatherer,
					strings.NewReader(test.wantMetrics),
				))
			} else {
				require.NoError(testutil.GatherAndCompare(
					gatherer,
					strings.NewReader(test.wantMetrics),
				))
			}
		})
	}
}

func TestLabelGatherer_Registration(t *testing.T) {
	const (
		firstName  = "first"
		secondName = "second"
	)
	firstLabeledGatherer := &labeledGatherer{
		labelValue: firstName,
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
		labelValue: secondName,
		gatherer: &testGatherer{
			mfs: []*dto.MetricFamily{{}},
		},
	}
	secondLabelGatherer := func() *labelGatherer {
		return &labelGatherer{
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
	}
	onlySecondLabeledGatherer := &labelGatherer{
		multiGatherer: multiGatherer{
			names: []string{
				secondLabeledGatherer.labelValue,
			},
			gatherers: prometheus.Gatherers{
				secondLabeledGatherer,
			},
		},
	}

	registerTests := []struct {
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
			labelValue:            firstName,
			gatherer:              firstLabeledGatherer.gatherer,
			expectedErr:           nil,
			expectedLabelGatherer: firstLabelGatherer(),
		},
		{
			name:                  "second registration",
			labelGatherer:         firstLabelGatherer(),
			labelValue:            secondName,
			gatherer:              secondLabeledGatherer.gatherer,
			expectedErr:           nil,
			expectedLabelGatherer: secondLabelGatherer(),
		},
		{
			name:                  "conflicts with previous registration",
			labelGatherer:         firstLabelGatherer(),
			labelValue:            firstName,
			gatherer:              secondLabeledGatherer.gatherer,
			expectedErr:           errDuplicateGatherer,
			expectedLabelGatherer: firstLabelGatherer(),
		},
	}
	for _, test := range registerTests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			err := test.labelGatherer.Register(test.labelValue, test.gatherer)
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.expectedLabelGatherer, test.labelGatherer)
		})
	}

	deregisterTests := []struct {
		name                  string
		labelGatherer         *labelGatherer
		labelValue            string
		expectedRemoved       bool
		expectedLabelGatherer *labelGatherer
	}{
		{
			name:                  "remove from nothing",
			labelGatherer:         &labelGatherer{},
			labelValue:            firstName,
			expectedRemoved:       false,
			expectedLabelGatherer: &labelGatherer{},
		},
		{
			name:                  "remove unknown name",
			labelGatherer:         firstLabelGatherer(),
			labelValue:            secondName,
			expectedRemoved:       false,
			expectedLabelGatherer: firstLabelGatherer(),
		},
		{
			name:            "remove first name",
			labelGatherer:   firstLabelGatherer(),
			labelValue:      firstName,
			expectedRemoved: true,
			expectedLabelGatherer: &labelGatherer{
				multiGatherer: multiGatherer{
					// We must populate with empty slices rather than nil slices
					// to pass the equality check.
					names:     []string{},
					gatherers: prometheus.Gatherers{},
				},
			},
		},
		{
			name:                  "remove second name",
			labelGatherer:         secondLabelGatherer(),
			labelValue:            secondName,
			expectedRemoved:       true,
			expectedLabelGatherer: firstLabelGatherer(),
		},
		{
			name:                  "remove only first name",
			labelGatherer:         secondLabelGatherer(),
			labelValue:            firstName,
			expectedRemoved:       true,
			expectedLabelGatherer: onlySecondLabeledGatherer,
		},
	}
	for _, test := range deregisterTests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			removed := test.labelGatherer.Deregister(test.labelValue)
			require.Equal(test.expectedRemoved, removed)
			require.Equal(test.expectedLabelGatherer, test.labelGatherer)
		})
	}
}
