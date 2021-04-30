// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pubsub

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"reflect"
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
	cmdMsg := &CommandMessage{}
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

func TestCommandMessage_Load(t *testing.T) {
	cmdMsg := &CommandMessage{}
	cmdMsg.Command = "test123"
	cmdMsg.Unsubscribe = true
	cmdMsg.Addresses = make([]string, 0, 1)
	cmdMsg.Addresses = append(cmdMsg.Addresses, "addr1")
	j, _ := json.Marshal(cmdMsg)

	cmdMsgTest := &CommandMessage{}
	err := cmdMsgTest.Load(bytes.NewReader(j))
	if err != nil {
		t.Fatalf("load command message failed")
	}
	if cmdMsgTest.Command != cmdMsg.Command {
		t.Fatalf("load command message failed")
	}
	if len(cmdMsgTest.Addresses) != 1 && cmdMsgTest.Addresses[1] != "addr1" {
		t.Fatalf("load command message failed")
	}
	if !reflect.DeepEqual(cmdMsg, cmdMsgTest) {
		t.Fatalf("load command message failed")
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
	fp.UpdateAddressMulti(false, bl...)
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

	if !fp.HasFilter() {
		t.Fatalf("has filter failed")
	}

	idsv := ids.GenerateTestShortID()
	fp.address[idsv] = struct{}{}
	if !fp.CheckAddress(idsv[:]) {
		t.Fatalf("check address failed")
	}
	delete(fp.address, idsv)

	mockFilter.Add([]byte("hello"))
	if !fp.CheckAddress([]byte("hello")) {
		t.Fatalf("check address failed")
	}
	if fp.CheckAddress([]byte("bye")) {
		t.Fatalf("check address failed")
	}
	idsv = ids.GenerateTestShortID()
	fp.UpdateAddress(false, idsv)
	if len(fp.address) != 1 {
		t.Fatalf("update address failed")
	}
	fp.UpdateAddress(true, idsv)
	if len(fp.address) != 0 {
		t.Fatalf("update address failed")
	}
}

func TestCommandMessage(t *testing.T) {
	cm := &CommandMessage{}
	if cm.IsNewFilter() {
		t.Fatalf("new filter check failed")
	}
	cm.FilterOrDefault()
	if cm.FilterMax != DefaultFilterMax && cm.FilterError != DefaultFilterError {
		t.Fatalf("default filter check failed")
	}
	cm.FilterMax = 1
	cm.FilterError = .1
	cm.FilterOrDefault()
	if cm.FilterMax != 1 && cm.FilterError != .1 {
		t.Fatalf("default filter check failed")
	}
}

func byteToID(address []byte) ids.ShortID {
	sid, err := ids.ToShortID(address)
	if err != nil {
		return ids.ShortEmpty
	}
	return sid
}
