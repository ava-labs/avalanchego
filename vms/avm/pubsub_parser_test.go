package avm

import (
	"encoding/hex"
	"testing"

	"github.com/ava-labs/avalanchego/ids"

	"github.com/ava-labs/avalanchego/pubsub"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
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

func TestFilter(t *testing.T) {
	idsid, _ := hex2Short("0000000000000000000000000000000000000001")

	tx := Tx{}
	baseTx := BaseTx{}
	to1 := &avax.TransferableOutput{Out: &secp256k1fx.TransferOutput{OutputOwners: secp256k1fx.OutputOwners{Addrs: []ids.ShortID{idsid}}}}
	baseTx.Outs = append(baseTx.Outs, to1)
	tx.UnsignedTx = &baseTx
	parser := NewPubSubParser(&tx)
	fp := pubsub.NewFilterParam()
	fp.UpdateAddress(false, idsid)
	fr := parser.Filter(fp)
	if fr == nil {
		t.Fatalf("filter failed")
	}
}
