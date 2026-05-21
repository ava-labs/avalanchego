// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package compression

import (
	"errors"
	"strings"
)

var errUnknownCompressionType = errors.New("unknown compression type")

type Type byte

const (
	TypeNone Type = iota + 1
	TypeZstd
)

func (t Type) String() string {
	switch t {
	case TypeNone:
		return "none"
	case TypeZstd:
		return "zstd"
	default:
		return "unknown"
	}
}

func TypeFromString(s string) (Type, error) {
	switch s {
	case TypeNone.String():
		return TypeNone, nil
	case TypeZstd.String():
		return TypeZstd, nil
	default:
		return TypeNone, errUnknownCompressionType
	}
}

func (t Type) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	if _, err := b.WriteString(`"`); err != nil {
		return nil, err
	}
	if _, err := b.WriteString(t.String()); err != nil {
		return nil, err
	}
	if _, err := b.WriteString(`"`); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}
