package topofblock

import (
	"math/rand/v2"
	"testing"

	"github.com/arr4n/shed/testerr"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/holiman/uint256"
)

func uint(x uint64) uint256.Int {
	return *uint256.NewInt(x)
}

func bid(x uint64) types.AccessTuple {
	return Bid(uint(x))
}

func list(ts ...types.AccessTuple) types.AccessList {
	return types.AccessList(ts)
}

func TestRoundTrip(t *testing.T) {
	double := bid(0)
	double.StorageKeys = append(double.StorageKeys, common.Hash{})

	empty := bid(0)
	empty.StorageKeys = nil

	tests := []struct {
		name    string
		list    types.AccessList
		want    uint256.Int
		wantOK  bool
		wantErr testerr.Want
	}{
		{
			name: "empty_access_list",
		},
		{
			name:   "zero_bid",
			list:   list(Bid(uint256.Int{})),
			wantOK: true,
		},
		{
			name:   "arbitrary_bid",
			list:   list(bid(42)),
			want:   uint(42),
			wantOK: true,
		},
		{
			name:   "with_other_access_tuples",
			list:   list(types.AccessTuple{}, bid(314159)),
			want:   uint(314159),
			wantOK: true,
		},
		{
			name:    "double_bid_in_different_tuples",
			list:    list(bid(0), bid(42)),
			wantErr: testerr.Is(ErrDuplicate),
		},
		{
			name:    "double_bid_in_same_tuple",
			list:    list(double),
			wantErr: testerr.Is(ErrDuplicate),
		},
		{
			name:    "empty_bid",
			list:    list(empty),
			wantErr: testerr.Is(ErrEmpty),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok, err := BidOf(types.NewTx(&types.AccessListTx{
				AccessList: tt.list,
			}))
			if diff := testerr.Diff(err, tt.wantErr); diff != "" {
				t.Fatalf("BidOf(...) %s", diff)
			}
			if !got.Eq(&tt.want) || ok != tt.wantOK {
				t.Errorf("BidOf(...) got (%v, %t); want (%v, %t)", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func FuzzRoundTrip(f *testing.F) {
	rng := rand.NewPCG(0, 0)
	u := rng.Uint64
	for range 100 {
		f.Add(u(), u(), u(), u())
	}

	f.Fuzz(func(t *testing.T, w0, w1, w2, w3 uint64) {
		bid := uint256.Int{w0, w1, w2, w3}
		tx := types.NewTx(&types.AccessListTx{
			AccessList: list(Bid(bid)),
		})

		got, ok, err := BidOf(tx)
		if err != nil || !ok || !got.Eq(&bid) {
			t.Errorf("BidOf(%+v) got (%v, %t, %v); want (%v, true, nil)", tx.AccessList(), got, ok, err, bid)
		}
	})
}
