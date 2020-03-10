// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package wrappers

// Errs ...
type Errs struct{ Err error }

// Errored ...
func (errs *Errs) Errored() bool { return errs.Err != nil }

// Add ...
func (errs *Errs) Add(errors ...error) {
	if errs.Err == nil {
		for _, err := range errors {
			if err != nil {
				errs.Err = err
				break
			}
		}
	}
}
