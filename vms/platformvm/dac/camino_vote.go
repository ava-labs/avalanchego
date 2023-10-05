// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package dac

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var _ Vote = (*DummyVote)(nil)

type Vote interface {
	verify.Verifiable

	VotedOptions() any
}
type VoteWithAddr struct {
	Vote         `serialize:"true"`
	VoterAddress ids.ShortID `serialize:"true"`
}

type DummyVote struct {
	ErrorStr string `serialize:"true"`
}

func (v *DummyVote) Verify() error {
	if v.ErrorStr != "" {
		return errors.New(v.ErrorStr)
	}
	return nil
}

func (*DummyVote) VotedOptions() any { return nil }
