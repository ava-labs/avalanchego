// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"fmt"
	"testing"

	stdjson "encoding/json"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

func TestMarshallUnmarshalNodeID(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("MarshalUnmarshalInversion", prop.ForAll(
		func(buf string) string {
			var (
				input  = NodeID(buf)
				output = new(NodeID)
			)

			// json package marshalling
			b, err := stdjson.Marshal(input)
			if err != nil {
				return err.Error()
			}
			if err := stdjson.Unmarshal(b, output); err != nil {
				return err.Error()
			}
			if input != *output {
				return fmt.Sprintf("broken inversion original %s, retrieved %s", input, *output)
			}

			// MarshalJson/UnmarshalJson
			output = new(NodeID)
			b, err = input.MarshalJSON()
			if err != nil {
				return err.Error()
			}
			if err := output.UnmarshalJSON(b); err != nil {
				return err.Error()
			}
			if input != *output {
				return fmt.Sprintf("broken inversion original %s, retrieved %s", input, *output)
			}

			return ""
		},
		gen.AnyString()),
	)

	properties.TestingRun(t)
}
