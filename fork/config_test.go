// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fork_test

import (
	"encoding/json"
	"fmt"
	"math"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/fork"
	"github.com/ava-labs/avalanchego/fork/forktest"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var testForkTime = time.Date(2026, time.October, 15, 15, 0, 0, 0, time.UTC)

func TestNew(t *testing.T) {
	valid := forktest.NewValidator(t, ids.GenerateTestNodeID(), 10)

	other := forktest.NewValidator(t, ids.GenerateTestNodeID(), 10)
	badPoP := forktest.NewValidator(t, ids.GenerateTestNodeID(), 10)
	badPoP.Signer = &signer.ProofOfPossession{
		PublicKey:         other.Signer.PublicKey, // valid key, but not the one that signed
		ProofOfPossession: badPoP.Signer.ProofOfPossession,
	}

	with := func(f func(v *fork.Validator)) fork.Validator {
		v := forktest.NewValidator(t, ids.GenerateTestNodeID(), 10)
		f(&v)
		return v
	}

	tests := []struct {
		name     string
		forkTime time.Time
		grace    time.Duration
		vdrs     []fork.Validator
		wantErr  error
	}{
		{name: "valid", forkTime: testForkTime, grace: fork.DefaultGracePeriod, vdrs: []fork.Validator{valid}},
		{name: "zero grace", forkTime: testForkTime, vdrs: []fork.Validator{valid}},
		{name: "missing fork time", grace: fork.DefaultGracePeriod, vdrs: []fork.Validator{valid}, wantErr: fork.ErrMissingForkTime},
		{name: "sub-second fork time", forkTime: testForkTime.Add(time.Millisecond), vdrs: []fork.Validator{valid}, wantErr: fork.ErrSubSecondForkTime},
		{name: "negative grace", forkTime: testForkTime, grace: -time.Second, vdrs: []fork.Validator{valid}, wantErr: fork.ErrNegativeGracePeriod},
		{name: "no validators", forkTime: testForkTime, wantErr: fork.ErrNoValidators},
		{name: "duplicate node ID", forkTime: testForkTime, vdrs: []fork.Validator{valid, valid}, wantErr: fork.ErrDuplicateNodeID},
		{name: "zero weight", forkTime: testForkTime, vdrs: []fork.Validator{with(func(v *fork.Validator) { v.Weight = 0 })}, wantErr: fork.ErrZeroWeight},
		{name: "missing signer", forkTime: testForkTime, vdrs: []fork.Validator{with(func(v *fork.Validator) { v.Signer = nil })}, wantErr: fork.ErrMissingSigner},
		{name: "invalid proof of possession", forkTime: testForkTime, vdrs: []fork.Validator{badPoP}, wantErr: signer.ErrInvalidProofOfPossession},
		{name: "missing IP", forkTime: testForkTime, vdrs: []fork.Validator{with(func(v *fork.Validator) { v.IP = netip.AddrPort{} })}, wantErr: fork.ErrMissingIP},
		{name: "zero port", forkTime: testForkTime, vdrs: []fork.Validator{with(func(v *fork.Validator) { v.IP = netip.AddrPortFrom(v.IP.Addr(), 0) })}, wantErr: fork.ErrMissingIP},
		{
			name:     "total weight overflow",
			forkTime: testForkTime,
			vdrs: []fork.Validator{
				with(func(v *fork.Validator) { v.Weight = math.MaxUint64 }),
				with(func(v *fork.Validator) { v.Weight = 1 }),
			},
			wantErr: safemath.ErrOverflow,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := fork.New(tt.forkTime, tt.grace, tt.vdrs)
			require.ErrorIs(t, err, tt.wantErr, "fork.New()")
			if tt.wantErr != nil {
				return
			}
			require.Equal(t, tt.forkTime.Add(tt.grace), c.SwitchTime(), "SwitchTime()")
			require.True(t, c.IsForked(tt.forkTime), "IsForked(T)")
			require.False(t, c.IsForked(tt.forkTime.Add(-time.Second)), "IsForked(T-1s)")
			require.Len(t, c.ValidatorSet(), len(tt.vdrs), "ValidatorSet()")
			nodeIDs := c.NodeIDs()
			require.True(t, nodeIDs.Contains(valid.NodeID), "NodeIDs()")
		})
	}
}

func TestNewSortsValidators(t *testing.T) {
	a := forktest.NewValidator(t, ids.BuildTestNodeID([]byte{2}), 1)
	b := forktest.NewValidator(t, ids.BuildTestNodeID([]byte{1}), 1)
	c, err := fork.New(testForkTime, 0, []fork.Validator{a, b})
	require.NoError(t, err, "fork.New()")
	require.Equal(t, []ids.NodeID{b.NodeID, a.NodeID}, []ids.NodeID{c.Validators[0].NodeID, c.Validators[1].NodeID}, "validator order")
}

func TestParseRoundTrip(t *testing.T) {
	want := forktest.NewConfig(t, testForkTime, ids.GenerateTestNodeID(), ids.GenerateTestNodeID())
	b, err := json.Marshal(want)
	require.NoError(t, err, "json.Marshal()")

	got, err := fork.Parse(b)
	require.NoError(t, err, "fork.Parse()")
	require.Equal(t, want.Hash(), got.Hash(), "Hash()")
	require.True(t, want.Time.Equal(got.Time), "Time")
	require.Equal(t, want.GracePeriod, got.GracePeriod, "GracePeriod")
}

func TestParseDefaultsGracePeriod(t *testing.T) {
	v := forktest.NewValidator(t, ids.GenerateTestNodeID(), 1)
	signerJSON, err := json.Marshal(v.Signer)
	require.NoError(t, err, "json.Marshal(signer)")
	doc := fmt.Sprintf(`{"forkTime":"2026-10-15T15:00:00Z","validators":[{"nodeID":%q,"weight":1,"signer":%s,"ip":"127.0.0.1:9651"}]}`,
		v.NodeID, signerJSON)

	c, err := fork.Parse([]byte(doc))
	require.NoError(t, err, "fork.Parse()")
	require.Equal(t, fork.DefaultGracePeriod, c.GracePeriod, "GracePeriod")
}

func TestParseInvalidJSON(t *testing.T) {
	_, err := fork.Parse([]byte("{"))
	var syntaxErr *json.SyntaxError
	require.ErrorAs(t, err, &syntaxErr, "fork.Parse()")
}

func TestParseHashStable(t *testing.T) {
	v1 := forktest.NewValidator(t, ids.BuildTestNodeID([]byte{1}), 1)
	v2 := forktest.NewValidator(t, ids.BuildTestNodeID([]byte{2}), 2)
	entry := func(v fork.Validator) string {
		s, err := json.Marshal(v.Signer)
		require.NoError(t, err, "json.Marshal(signer)")
		return fmt.Sprintf(`{"nodeID":%q,"weight":%d,"signer":%s,"ip":"127.0.0.1:9651"}`, v.NodeID, v.Weight, s)
	}
	parse := func(doc string) [32]byte {
		c, err := fork.Parse([]byte(doc))
		require.NoError(t, err, "fork.Parse(%s)", doc)
		return c.Hash()
	}

	base := parse(`{"forkTime":"2026-10-15T15:00:00Z","gracePeriod":"61s","validators":[` + entry(v1) + "," + entry(v2) + `]}`)

	t.Run("validator order", func(t *testing.T) {
		got := parse(`{"forkTime":"2026-10-15T15:00:00Z","gracePeriod":"61s","validators":[` + entry(v2) + "," + entry(v1) + `]}`)
		require.Equal(t, base, got, "Hash()")
	})
	t.Run("whitespace and field order", func(t *testing.T) {
		got := parse(`{ "validators": [` + entry(v1) + ",\n" + entry(v2) + `], "gracePeriod": "1m1s", "forkTime": "2026-10-15T15:00:00Z" }`)
		require.Equal(t, base, got, "Hash()")
	})
	t.Run("timezone", func(t *testing.T) {
		got := parse(`{"forkTime":"2026-10-15T17:00:00+02:00","gracePeriod":"61s","validators":[` + entry(v1) + "," + entry(v2) + `]}`)
		require.Equal(t, base, got, "Hash()")
	})
	t.Run("weight change", func(t *testing.T) {
		v2b := v2
		v2b.Weight = 3
		got := parse(`{"forkTime":"2026-10-15T15:00:00Z","gracePeriod":"61s","validators":[` + entry(v1) + "," + entry(v2b) + `]}`)
		require.NotEqual(t, base, got, "Hash()")
	})
}
