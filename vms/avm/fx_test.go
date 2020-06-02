// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

type testFx struct {
	initialize, bootstrapping, bootstrapped, verifyTransfer, verifyOperation error
}

func (fx *testFx) Initialize(_ interface{}) error              { return fx.initialize }
func (fx *testFx) Bootstrapping() error                        { return fx.bootstrapping }
func (fx *testFx) Bootstrapped() error                         { return fx.bootstrapped }
func (fx *testFx) VerifyTransfer(_, _, _, _ interface{}) error { return fx.verifyTransfer }
func (fx *testFx) VerifyOperation(_, _, _ interface{}, _ []interface{}) error {
	return fx.verifyOperation
}
