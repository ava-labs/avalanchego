// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"strings"

	"go.uber.org/zap"
)

type sanitizedString string

func (s sanitizedString) String() string {
	return strings.ReplaceAll(string(s), "\n", `\n`)
}

// UserString constructs a field with the given key and the value stripped of
// newlines. The value is sanitized lazily.
func UserString(key, val string) zap.Field {
	return zap.Stringer(key, sanitizedString(val))
}

type sanitizedStrings []string

func (s sanitizedStrings) String() string {
	var strs strings.Builder
	for i, str := range s {
		if i != 0 {
			_, _ = strs.WriteString(", ")
		}
		_, _ = strs.WriteString(strings.ReplaceAll(str, "\n", `\n`))
	}
	return strs.String()
}

// UserStrings constructs a field with the given key and the values stripped of
// newlines. The values are sanitized lazily.
func UserStrings(key string, val []string) zap.Field {
	return zap.Stringer(key, sanitizedStrings(val))
}
