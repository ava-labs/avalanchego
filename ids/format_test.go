// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"fmt"
	"testing"
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
				if got := fmt.Sprintf(format, tt.id); got != want {
					//nolint:forbidigo // require.Equal() is inappropriate as it unnecessarily stops future tests and `assert.Equal()` isn't allowed
					t.Errorf("fmt.Sprintf(%q, %T) got %q; want %q", format, tt.id, got, want)
				}
			}
		})
	}
}

func BenchmarkFormat(*testing.B) {
	// %q uses a []byte so this is just to demonstrate that it's on the stack
	// otherwise someone, not naming any names, might want to "fix" it.
	_ = fmt.Sprintf("%q", ID{})
	_ = fmt.Sprintf("%q", ShortID{})
}
