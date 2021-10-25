// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

var (
	_ TxError = &tempError{}
	_ TxError = &permError{}
)

// TxError provides the ability for errors to be distinguished as permanent or
// temporary
type TxError interface {
	error
	Unwrap() error
	Temporary() bool
}

type tempError struct{ error }

func (e tempError) Unwrap() error { return e.error }
func (tempError) Temporary() bool { return true }

type permError struct{ error }

func (e permError) Unwrap() error { return e.error }
func (permError) Temporary() bool { return false }
