// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

// TODO(marun) What else does a test need? e.g. node URIs?
type APITestFunction func(tc tests.TestContext, wallet primary.Wallet, ownerAddress ids.ShortID)

// ExecuteAPITest executes a test primary dependency is being able to access the API of one or
// more avalanchego nodes.
func ExecuteAPITest(apiTest APITestFunction) {
	tc := NewTestContext()
	env := GetEnv(tc)
	keychain := env.NewKeychain()
	wallet := NewWallet(tc, keychain, env.GetRandomNodeURI())
	apiTest(tc, *wallet, keychain.Keys[0].Address())
	_ = CheckBootstrapIsPossible(tc, env.GetNetwork())
}
