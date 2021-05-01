// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pubsub

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/stretchr/testify/assert"
)

func TestAddAddressesParseAddresses(t *testing.T) {
	assert := assert.New(t)

	chainAlias := "X"
	hrp := constants.GetHRP(5)

	addrID := ids.ShortID{1}
	addrStr, err := formatting.FormatAddress(chainAlias, hrp, addrID[:])
	assert.NoError(err)

	msg := &AddAddresses{
		Addresses: []string{
			addrStr,
		},
	}

	err = msg.parseAddresses()
	assert.NoError(err)

	assert.Len(msg.addressIds, 1)
	assert.Equal(addrID[:], msg.addressIds[0])
}

func TestFilterParamUpdateMulti(t *testing.T) {
	fp := NewFilterParam()
	bl := make([][]byte, 0, 10)

	addr1 := byteToID(hashing.ComputeHash160([]byte("abc")))
	addr2 := byteToID(hashing.ComputeHash160([]byte("def")))
	addr3 := byteToID(hashing.ComputeHash160([]byte("xyz")))

	bl = append(bl, addr1[:])
	bl = append(bl, addr2[:])
	bl = append(bl, addr3[:])
	_ = fp.Add(bl...)
	if len(fp.address) != 3 {
		t.Fatalf("update multi failed")
	}
	if _, exists := fp.address[addr1]; !exists {
		t.Fatalf("update multi failed")
	}
	if _, exists := fp.address[addr2]; !exists {
		t.Fatalf("update multi failed")
	}
	if _, exists := fp.address[addr3]; !exists {
		t.Fatalf("update multi failed")
	}
}

func TestFilterParam(t *testing.T) {
	mapFilter := bloom.NewMap()

	fp := NewFilterParam()
	fp.filter = mapFilter

	idsv := ids.GenerateTestShortID()
	fp.address[idsv] = struct{}{}
	if !fp.Check(idsv[:]) {
		t.Fatalf("check address failed")
	}
	delete(fp.address, idsv)

	mapFilter.Add(idsv[:])
	if !fp.Check(idsv[:]) {
		t.Fatalf("check address failed")
	}
	if fp.Check([]byte("bye")) {
		t.Fatalf("check address failed")
	}
}

func TestNewBloom(t *testing.T) {
	cm := &NewBloom{}
	if cm.IsParamsValid() {
		t.Fatalf("new filter check failed")
	}
}

func byteToID(address []byte) ids.ShortID {
	sid, err := ids.ToShortID(address)
	if err != nil {
		return ids.ShortEmpty
	}
	return sid
}
