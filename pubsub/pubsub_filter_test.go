package pubsub

import (
	"encoding/hex"
	"net/http"
	"net/url"
	"testing"

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

func TestFilter(t *testing.T) {
	idaddr1 := "FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF"
	idaddr2 := "0000000000000000000000000000000000000001"

	pubsubfilter := &pubsubfilter{}
	r := http.Request{}
	r.URL = &url.URL{RawQuery: "address=" + idaddr1 + "&" +
		"address=" + idaddr2 + ""}
	fp := pubsubfilter.buildFilter(&r)
	if fp == nil || len(fp.Address) != 2 {
		t.Fatalf("build filter failed")
	}
	ids1, _ := hex2Short(idaddr1)
	if fp.Address[0] != ids1 {
		t.Fatalf("build filter failed %s", "0x"+idaddr1)
	}
	ids2, _ := hex2Short(idaddr2)
	if fp.Address[1] != ids2 {
		t.Fatalf("build filter failed %s", idaddr2)
	}
}
