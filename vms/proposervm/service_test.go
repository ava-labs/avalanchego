// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"testing"
	"time"
)

func defaultAPI(t *testing.T) *ProposerAPI {
	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	_, _, vm, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	return &ProposerAPI{
		vm: vm,
	}
}

func TestGetProposedHeight(t *testing.T) {
	// require := require.New(t)

}
