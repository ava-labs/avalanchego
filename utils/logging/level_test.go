// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTo5Chars(t *testing.T) {
	{
		// Case: len(s) < 5
		s := "1234"
		padded := to5Chars(s)
		assert.Equal(t, "1234 ", padded)
	}
	{
		// Case: len(s) == 5
		s := "12345"
		padded := to5Chars(s)
		assert.Equal(t, "12345", padded)
	}
	{
		// Case: len(s) > 5
		s := "123456"
		padded := to5Chars(s)
		assert.Equal(t, "12345", padded)
	}
}

func TestLevelMarshalUnmarshalJSON(t *testing.T) {
	levels := []Level{Off, Fatal, Error, Warn, Info, Trace, Debug, Verbo}
	for _, l := range levels {
		asJSON, err := json.Marshal(l)
		assert.NoError(t, err)
		assert.EqualValues(t, fmt.Sprintf("\"%s\"", l), string(asJSON))
		var gotL Level
		err = json.Unmarshal(asJSON, &gotL)
		assert.NoError(t, err)
		assert.Equal(t, l, gotL)
	}
}
