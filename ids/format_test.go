// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormat(t *testing.T) {
	type test struct {
		id   idForFormatting
		want map[string]string // format -> output
	}
	makeTestCase := func(id idForFormatting) test {
		return test{
			id: id,
			want: map[string]string{
				"%v":  id.String(),
				"%s":  id.String(),
				"%q":  `"` + id.String() + `"`,
				"%x":  id.Hex(),
				"%#x": `0x` + id.Hex(),
			},
		}
	}

	tests := []test{
		makeTestCase(ID{}),
		makeTestCase(GenerateTestID()),
		makeTestCase(GenerateTestID()),
		makeTestCase(ShortID{}),
		makeTestCase(GenerateTestShortID()),
		makeTestCase(GenerateTestShortID()),
	}

	for _, tt := range tests {
		t.Run(tt.id.String(), func(t *testing.T) {
			for format, want := range tt.want {
				require.Equalf(t, want, fmt.Sprintf(format, tt.id), "fmt.Sprintf(%q, %T)", format, tt.id)
			}
		})
	}
}
