// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestUnmarshalConfig(t *testing.T) {
	tests := []struct {
		name        string
		givenJSON   []byte
		expected    Config
		expectedErr bool
	}{
		{
			"string durations parsed",
			[]byte(`{"api-max-duration": "1m", "continuous-profiler-frequency": "2m"}`),
			Config{APIMaxDuration: Duration{1 * time.Minute}, ContinuousProfilerFrequency: Duration{2 * time.Minute}},
			false,
		},
		{
			"integer durations parsed",
			[]byte(fmt.Sprintf(`{"api-max-duration": "%v", "continuous-profiler-frequency": "%v"}`, 1*time.Minute, 2*time.Minute)),
			Config{APIMaxDuration: Duration{1 * time.Minute}, ContinuousProfilerFrequency: Duration{2 * time.Minute}},
			false,
		},
		{
			"bad durations",
			[]byte(`{"api-max-duration": "bad-duration"}`),
			Config{},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tmp Config
			err := json.Unmarshal(tt.givenJSON, &tmp)
			if tt.expectedErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, tmp)
			}
		})
	}
}
