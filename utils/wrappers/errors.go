// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package wrappers

type Errs struct{ Err error }

func (errs *Errs) Errored() bool {
	return errs.Err != nil
}

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
