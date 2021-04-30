package bloom

import (
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

	f.Add([]byte("hello"))
	if !f.Check([]byte("hello")) {
		t.Fatal("check failed")
	}
	if f.Check([]byte("bye")) {
		t.Fatal("check failed")
	}
}
