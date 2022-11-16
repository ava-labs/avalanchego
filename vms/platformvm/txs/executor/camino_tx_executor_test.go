// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"

	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
)

func TestCaminoEnv(t *testing.T) {
	caminoGenesisConf := genesis.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}
	env := newCaminoEnvironment( /*postBanff*/ true, caminoGenesisConf)
	env.ctx.Lock.Lock()
	defer func() {
		if err := shutdownEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()
	env.config.BanffTime = env.state.GetTimestamp()
}
