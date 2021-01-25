package pubsub

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

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

func TestCommandMessage_TransposeAddress(t *testing.T) {
	hrp := constants.GetHRP(5)
	cmdMsg := &CommandMessage{}
	cmdMsg.AddressIds = make([][]byte, 0, 1)
	idsid1, _ := hex2Short("0000000000000000000000000000000000000001")
	b32addr, _ := formatting.FormatBech32(hrp, idsid1[:])
	if !strings.HasPrefix(b32addr, hrp) {
		t.Fatalf("address transpose failed")
	}
	cmdMsg.Addresses = append(cmdMsg.Addresses, "Z-"+b32addr)
	cmdMsg.TransposeAddress(hrp)
	if !bytes.Equal(cmdMsg.AddressIds[0], idsid1[:]) {
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

func TestCompare(t *testing.T) {
	idsid1, _ := hex2Short("0000000000000000000000000000000000000001")
	idsid2, _ := hex2Short("0000000000000000000000000000000000000001")

	if !compare(idsid1, idsid2[:]) {
		t.Fatalf("filter failed")
	}

	idsid3, _ := hex2Short("0000000000000000000000000000000000000010")

	if compare(idsid1, idsid3[:]) {
		t.Fatalf("filter failed")
	}

	idsid1, _ = hex2Short("0000000000000000000000000000000000000011")
	idsid4, _ := hex2Short("0000000000000000000000000000000000000001")

	if !compare(idsid1, idsid4[:]) {
		t.Fatalf("filter failed")
	}

	idsid1, _ = hex2Short("0000000000000000000000000000000000000011")
	idsid4, _ = hex2Short("0000000000000000000000000000000000000010")

	if !compare(idsid1, idsid4[:]) {
		t.Fatalf("filter failed")
	}
}

func TestFilterParamUpdateMulti(t *testing.T) {
	fp := NewFilterParam()
	bl := make([][]byte, 0, 10)
	bl = append(bl, []byte("abc"))
	bl = append(bl, []byte("def"))
	bl = append(bl, []byte("xyz"))
	fp.UpdateAddressMulti(false, bl...)
	if len(fp.address) != 3 {
		t.Fatalf("update multi failed")
	}
	if _, exists := fp.address[ByteToID([]byte("abc"))]; !exists {
		t.Fatalf("update multi failed")
	}
	if _, exists := fp.address[ByteToID([]byte("def"))]; !exists {
		t.Fatalf("update multi failed")
	}
	if _, exists := fp.address[ByteToID([]byte("xyz"))]; !exists {
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
