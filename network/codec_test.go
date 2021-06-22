// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

var TestCodec Codec

func TestCodecPackInvalidOp(t *testing.T) {
	_, err := TestCodec.Pack(nil, math.MaxUint8, make(map[Field]interface{}), false)
	assert.Error(t, err)
}

func TestCodecPackMissingField(t *testing.T) {
	_, err := TestCodec.Pack(nil, Get, make(map[Field]interface{}), false)
	assert.Error(t, err)
}

func TestCodecParseInvalidOp(t *testing.T) {
	_, err := TestCodec.Parse([]byte{math.MaxUint8}, true)
	assert.Error(t, err)
}

func TestCodecParseExtraSpace(t *testing.T) {
	_, err := TestCodec.Parse([]byte{byte(GetVersion), 0x00}, true)
	assert.Error(t, err)
}
