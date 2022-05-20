// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPrivateKeyFromString(t *testing.T) {
	assert := assert.New(t)
	pk := PrivateKey{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'}
	pkStr := pk.String()
	pk2, err := PrivateKeyFromString(pkStr)
	assert.NoError(err)
	assert.Equal(pk, pk2)
	expected := "PrivateKey-2qg4x8qM2s2qGNSXG"
	assert.Equal(expected, pkStr)
}

func TestPrivateKeyFromStringError(t *testing.T) {
	assert := assert.New(t)
	tests := []struct {
		in string
	}{
		{""},
		{"foo"},
		{"foobar"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			_, err := PrivateKeyFromString(tt.in)
			assert.Error(err)
		})
	}
}

func TestPrivateKeyMarshalJSON(t *testing.T) {
	assert := assert.New(t)
	tests := []struct {
		label string
		in    PrivateKey
		out   []byte
		err   error
	}{
		{"PrivateKey{}", PrivateKey{}, []byte("\"PrivateKey-45PJLL\""), nil},
		{
			"PrivateKey(\"ava labs\")",
			PrivateKey{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
			[]byte("\"PrivateKey-2qg4x8qM2s2qGNSXG\""),
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			out, err := tt.in.MarshalJSON()
			assert.Equal(err, tt.err)
			assert.Equal(out, tt.out)
		})
	}
}

func TestPrivateKeyUnmarshalJSON(t *testing.T) {
	assert := assert.New(t)
	tests := []struct {
		label     string
		in        []byte
		out       PrivateKey
		shouldErr bool
	}{
		{"PrivateKey{}", []byte("null"), PrivateKey{}, false},
		{
			"PrivateKey(\"ava labs\")",
			[]byte("\"PrivateKey-2qg4x8qM2s2qGNSXG\""),
			PrivateKey{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
			false,
		},
		{
			"missing start quote",
			[]byte("PrivateKey-9tLMkeWFhWXd8QZc4rSiS5meuVXF5kRsz\""),
			PrivateKey{},
			true,
		},
		{
			"missing end quote",
			[]byte("\"PrivateKey-9tLMkeWFhWXd8QZc4rSiS5meuVXF5kRsz"),
			PrivateKey{},
			true,
		},
		{
			"PrivateKey-",
			[]byte("\"PrivateKey-\""),
			PrivateKey{},
			true,
		},
		{
			"PrivateKey-1",
			[]byte("\"PrivateKey-1\""),
			PrivateKey{},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			foo := PrivateKey{}
			fmt.Println(foo.String())
			err := foo.UnmarshalJSON(tt.in)
			if tt.shouldErr {
				assert.Error(err)
			} else {
				assert.NoError(err)
			}
			assert.Equal(foo, tt.out)
		})
	}
}

func TestPrivateKeyString(t *testing.T) {
	assert := assert.New(t)
	tests := []struct {
		label    string
		id       PrivateKey
		expected string
	}{
		{"PrivateKey{}", PrivateKey{}, "PrivateKey-45PJLL"},
		{"PrivateKey{24}", PrivateKey{24}, "PrivateKey-3nv2Q5L"},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			result := tt.id.String()
			assert.Equal(result, tt.expected)
		})
	}
}

func TestSortPrivateKeys(t *testing.T) {
	assert := assert.New(t)
	pks := []PrivateKey{
		{'e', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
		{'W', 'a', 'l', 'l', 'e', ' ', 'l', 'a', 'b', 's'},
		{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
	}
	SortPrivateKeys(pks)
	expected := []PrivateKey{
		{'W', 'a', 'l', 'l', 'e', ' ', 'l', 'a', 'b', 's'},
		{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
		{'e', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
	}
	assert.Equal(pks, expected)
}
