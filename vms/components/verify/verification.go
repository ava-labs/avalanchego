// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package verify

import "github.com/ava-labs/avalanchego/snow"

type Verifiable interface {
	Verify() error
}

type State interface {
	snow.ContextInitializable
	Verifiable
	IsState
}

type IsState interface {
	isState()
}

type IsNotState interface {
	isState() error
}

// All returns nil if all the verifiables were verified with no errors
func All(verifiables ...Verifiable) error {
	for _, verifiable := range verifiables {
		if err := verifiable.Verify(); err != nil {
			return err
		}
	}
	return nil
}
