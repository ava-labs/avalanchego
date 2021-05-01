// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pubsub

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/ava-labs/avalanchego/utils/hashing"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"

	"github.com/ava-labs/avalanchego/utils/bloom"

	"github.com/ava-labs/avalanchego/ids"
)

func hex2Short(v string) (ids.ShortID, error) {
	b, err := hex.DecodeString(v)
	if err != nil {
		return ids.ShortEmpty, err
	}
	idsid, err := ids.ToShortID(b)
	if err != nil {
		return ids.ShortEmpty, err
	}
	return idsid, nil
}

func TestCommandMessage_ParseAddresses(t *testing.T) {
	hrp := constants.GetHRP(5)
	cmdMsg := &AddAddresses{}
	cmdMsg.addressIds = make([][]byte, 0, 1)
	idsid1, _ := hex2Short("0000000000000000000000000000000000000001")
	b32addr, _ := formatting.FormatBech32(hrp, idsid1[:])
	if !strings.HasPrefix(b32addr, hrp) {
		t.Fatalf("address transpose failed")
	}
	cmdMsg.Addresses = append(cmdMsg.Addresses, "Z-"+b32addr)
	_ = cmdMsg.ParseAddresses()
	if !bytes.Equal(cmdMsg.addressIds[0], idsid1[:]) {
		t.Fatalf("address transpose failed")
	}
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
	_ = fp.AddAddresses(bl...)
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
	mockFilter := bloom.NewMock()

	fp := NewFilterParam()
	fp.filter = mockFilter

	idsv := ids.GenerateTestShortID()
	fp.address[idsv] = struct{}{}
	if !fp.CheckAddress(idsv[:]) {
		t.Fatalf("check address failed")
	}
	delete(fp.address, idsv)

	mockFilter.Add(idsv[:])
	if !fp.CheckAddress(idsv[:]) {
		t.Fatalf("check address failed")
	}
	if fp.CheckAddress([]byte("bye")) {
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
