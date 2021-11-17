// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAlignedString(t *testing.T) {
	levels := []Level{Off, Fatal, Error, Warn, Info, Trace, Debug, Verbo}
	for _, l := range levels {
		as := l.AlignedString()
		assert.Len(t, as, alignedStringLen)
		s := l.String()
		switch {
		case len(s) >= alignedStringLen:
			assert.Equal(t, s[:alignedStringLen], as)
		default:
			assert.Equal(t, s, as[:len(s)])
			assert.Equal(t, as[len(s):], strings.Repeat(" ", alignedStringLen-len(s)))
		}
	}
}
