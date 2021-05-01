package avm

import (
	"encoding/hex"
	"testing"

	"github.com/ava-labs/avalanchego/pubsub"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	"github.com/ava-labs/avalanchego/ids"
)

func hex2Short(v string) (ids.ShortID, error) {
	bytes, err := hex.DecodeString(v)
	if err != nil {
		return ids.ShortEmpty, err
	}
	idsid, err := ids.ToShortID(bytes)
	if err != nil {
		return ids.ShortEmpty, err
	}
	return idsid, nil
}

type MockFilterInterface struct {
	ids ids.ShortID
}

func (f *MockFilterInterface) CheckAddress(addr ids.ShortID) bool {
	return addr == f.ids
}

func TestFilter(t *testing.T) {
	idsid, _ := hex2Short("0000000000000000000000000000000000000001")

	tx := Tx{}
	baseTx := BaseTx{}
	to1 := &avax.TransferableOutput{Out: &secp256k1fx.TransferOutput{OutputOwners: secp256k1fx.OutputOwners{Addrs: []ids.ShortID{idsid}}}}
	baseTx.Outs = append(baseTx.Outs, to1)
	tx.UnsignedTx = &baseTx
	parser := NewPubSubParser(&tx)
	fp := pubsub.NewFilterParam()
	_ = fp.AddAddresses([][]byte{idsid[:]}...)
	fr, _ := parser.Filter([]pubsub.FilterInterface{&MockFilterInterface{ids: idsid}})
	if len(fr) != 1 {
		t.Fatalf("filter failed")
	}
}
