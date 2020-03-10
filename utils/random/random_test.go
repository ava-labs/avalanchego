// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package random

import (
	"testing"
)

func TestNewMasterseed(t *testing.T) {
	seed, err := NewMasterseed()
	if err != nil {
		t.Fatalf("GetMasterseed returned error, either there is a lack of" +
			" entropy or something went wrong.")
	}

	hadOnes := false
	hadZeros := false
	for _, b := range seed {
		hadOnes = hadOnes || (b > 0)
		hadZeros = hadZeros || (b <= 255)
	}
	if !hadOnes {
		t.Fatalf("GetMasterseed doesn't return ones. Something is very wrong.")
	}
	if !hadZeros {
		t.Fatalf("GetMasterseed doesn't return zeros. Something is very wrong.")
	}
}

func TestNewNonce(t *testing.T) {
	max := uint64(0)
	hadEven := false
	hadOdd := false
	for i := 0; i < 100; i++ {
		nonce, err := NewNonce()
		if err != nil {
			t.Fatalf("NewNonce returned error, either there is a lack of" +
				" entropy or something went wrong.")
		}
		if nonce > max {
			max = nonce
		}
		hadEven = hadEven || (nonce%2 == 0)
		hadOdd = hadOdd || (nonce%2 == 1)
	}
	// The probabilities are related, but they act as a rule of thumb, the
	// probabilities are negligible.
	if max < 9223372036854775808 {
		t.Fatalf("GetNonce doesn't range from [0, 2^64 - 1] with p = 1 - 2^-100")
	}
	if !hadEven {
		t.Fatalf("GetNonce doesn't have even numbers with p = 1 - 2^-100")
	}
	if !hadOdd {
		t.Fatalf("GetNonce doesn't have odd numbers with p = 1 - 2^-100")
	}
}
