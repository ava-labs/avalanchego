// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

type testFx struct {
	initialize, verifyTransfer, verifyOperation error
}

func (fx *testFx) Initialize(_ interface{}) error              { return fx.initialize }
func (fx *testFx) VerifyTransfer(_, _, _, _ interface{}) error { return fx.verifyTransfer }
func (fx *testFx) VerifyOperation(_, _, _ interface{}, _ []interface{}) error {
	return fx.verifyOperation
}
