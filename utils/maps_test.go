// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMarshalMapSlice(t *testing.T) {
	tests := map[string]struct {
		input interface{}

		expected []map[string]interface{}
		err      string
	}{
		"basic string": {
			input: `[{"test": "hello"}]`,
			expected: []map[string]interface{}{
				{"test": "hello"},
			},
		},
		"basic array": {
			input: []interface{}{
				map[string]interface{}{
					"test": "hello",
				},
			},
			expected: []map[string]interface{}{
				{"test": "hello"},
			},
		},
		"string array": {
			input: `["test", "hello"]`,
			err:   "could not unmarshal `[\"test\", \"hello\"]` to []map[string]interface{}",
		},
		"int interface": {
			input: []interface{}{
				1, 2, 3,
			},
			err: "could not cast object at index 0 (`1`) to map[string]interface{}",
		},
		"unexpected type": {
			input: 1,
			err:   "could not marshal []map[string]interface{} for type `int`",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			m, err := MarshalMapSlice(test.input)
			if len(test.err) > 0 {
				assert.Error(err)
				assert.Contains(err.Error(), test.err)
				assert.Nil(m)
				return
			}
			assert.NoError(err)
			assert.Equal(test.expected, m)
		})
	}
}
