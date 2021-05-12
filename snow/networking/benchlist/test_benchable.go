// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package benchlist

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

type TestBenchable struct {
	T *testing.T

	CantBenched, CantUnbenched bool
	BenchedF, UnbenchedF       func(chainID ids.ID, validatorID ids.ShortID)
}

func (b *TestBenchable) Benched(chainID ids.ID, validatorID ids.ShortID) {
	if b.BenchedF != nil {
		b.BenchedF(chainID, validatorID)
	} else if b.CantBenched && b.T != nil {
		b.T.Fatalf("Unexpectedly called Benched")
	}
}
func (b *TestBenchable) Unbenched(chainID ids.ID, validatorID ids.ShortID) {
	if b.UnbenchedF != nil {
		b.UnbenchedF(chainID, validatorID)
	} else if b.CantUnbenched && b.T != nil {
		b.T.Fatalf("Unexpectedly called Unbenched")
	}
}
