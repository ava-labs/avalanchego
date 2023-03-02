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
	NoCompression Type = iota + 1
	GzipCompression
	ZstdCompression
)

func (t Type) String() string {
	switch t {
	case NoCompression:
		return "none"
	case GzipCompression:
		return "gzip"
	case ZstdCompression:
		return "zstd"
	default:
		return "unknown"
	}
}

func TypeFromString(s string) (Type, error) {
	switch s {
	case NoCompression.String():
		return NoCompression, nil
	case GzipCompression.String():
		return GzipCompression, nil
	case ZstdCompression.String():
		return ZstdCompression, nil
	default:
		return NoCompression, errUnknownCompressionType
	}
}

func (t Type) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	_, err := b.WriteString("\"")
	if err != nil {
		return nil, err
	}
	switch t {
	case NoCompression, GzipCompression, ZstdCompression:
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
