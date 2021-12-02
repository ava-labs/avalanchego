// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package json

import (
	"fmt"
	"testing"
)

func TestFloat32(t *testing.T) {
	type test struct {
		f                    Float32
		expectedStr          string
		expectedUnmarshalled float32
	}

	tests := []test{
		{
			0,
			"0.0000",
			0,
		}, {
			0.00001,
			"0.0000",
			0,
		}, {
			1,
			"1.0000",
			1,
		}, {
			1.0001,
			"1.0001",
			1.0001,
		}, {
			100.0000,
			"100.0000",
			100.0000,
		}, {
			100.0001,
			"100.0001",
			100.0001,
		},
	}

	for _, tt := range tests {
		jsonBytes, err := tt.f.MarshalJSON()
		if err != nil {
			t.Fatalf("couldn't marshal %f: %s", float32(tt.f), err)
		} else if string(jsonBytes) != fmt.Sprintf("\"%s\"", tt.expectedStr) {
			t.Fatalf("expected %f to marshal to %s but got %s", tt.f, tt.expectedStr, string(jsonBytes))
		}

		var f Float32
		if err := f.UnmarshalJSON(jsonBytes); err != nil {
			t.Fatalf("couldn't unmarshal %s to Float32: %s", string(jsonBytes), err)
		} else if float32(f) != tt.expectedUnmarshalled {
			t.Fatalf("expected %s to unmarshal to %f but got %f", string(jsonBytes), tt.expectedUnmarshalled, f)
		}
	}
}
