package bloom

import (
	"encoding/json"
	"testing"

	streakKnife "github.com/steakknife/bloomfilter"
)

const MaxBytes = 1 * 1024 * 1024

func TestSteakKnifeFilterSize(t *testing.T) {
	var maxN uint64 = 10000
	var p = 0.1
	m := streakKnife.OptimalM(maxN, p)
	k := streakKnife.OptimalK(m, maxN)
	msize := (m + 63) / 64
	msize += k
	f, _ := NewSteakKnifeFilter(maxN, p)
	j, _ := f.MarshalJSON()
	sk := &SteakKnifeJSON{}
	_ = json.Unmarshal(j, sk)

	if BytesSteakKnifeFilter(maxN, p) != msize*8 {
		t.Fatal("size calculation failed")
	}
	if uint64(len(sk.Keys)) != k {
		t.Fatal("size calculation failed")
	}
	if (uint64(len(sk.Bits)) + uint64(len(sk.Keys))) != msize {
		t.Fatal("size calculation failed")
	}

	f.Add([]byte("hello"))
	if !f.Check([]byte("hello")) {
		t.Fatal("check failed")
	}
	if f.Check([]byte("bye")) {
		t.Fatal("check failed")
	}
}
