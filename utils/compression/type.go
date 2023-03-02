// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
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
	TypeGzip
	TypeZstd
)

func (t Type) String() string {
	switch t {
	case TypeNone:
		return "none"
	case TypeGzip:
		return "gzip"
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
	case TypeGzip.String():
		return TypeGzip, nil
	case TypeZstd.String():
		return TypeZstd, nil
	default:
		return TypeNone, errUnknownCompressionType
	}
}

func (t Type) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	_, err := b.WriteString("\"")
	if err != nil {
		return nil, err
	}
	switch t {
	case TypeNone, TypeGzip, TypeZstd:
		_, err = b.WriteString(t.String())
	default:
		err = errUnknownCompressionType
	}
	if err != nil {
		return nil, err
	}
	_, err = b.WriteString("\"")
	if err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}
