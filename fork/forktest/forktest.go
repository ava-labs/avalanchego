// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package forktest provides helpers for testing fork mode.
package forktest

import (
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/fork"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
)

// NewValidator returns a fork validator with a fresh BLS key, a valid proof
// of possession, and a loopback IP.
func NewValidator(t testing.TB, nodeID ids.NodeID, weight uint64) fork.Validator {
	sk, err := localsigner.New()
	require.NoError(t, err, "localsigner.New()")
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(t, err, "signer.NewProofOfPossession()")
	return fork.Validator{
		NodeID: nodeID,
		Weight: weight,
		Signer: pop,
		IP:     netip.AddrPortFrom(netip.AddrFrom4([4]byte{127, 0, 0, 1}), 9651),
	}
}

// NewConfig returns a valid fork config at [forkTime] with one weight-1
// validator per node ID and the default grace period.
func NewConfig(t testing.TB, forkTime time.Time, nodeIDs ...ids.NodeID) *fork.Config {
	vdrs := make([]fork.Validator, len(nodeIDs))
	for i, nodeID := range nodeIDs {
		vdrs[i] = NewValidator(t, nodeID, 1)
	}
	c, err := fork.New(forkTime, fork.DefaultGracePeriod, vdrs)
	require.NoError(t, err, "fork.New()")
	return c
}
