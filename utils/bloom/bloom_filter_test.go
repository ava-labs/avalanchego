package bloom

import (
	"testing"
)

const MaxBytes = 1 * 1024 * 1024

func TestSteakKnifeFilterSize(t *testing.T) {
	var maxN uint64 = 10000
	var p = 0.1
	f, _ := NewSteakKnifeFilter(maxN, p)

	f.Add([]byte("hello"))
	if !f.Check([]byte("hello")) {
		t.Fatal("check failed")
	}
	if f.Check([]byte("bye")) {
		t.Fatal("check failed")
	}
}
