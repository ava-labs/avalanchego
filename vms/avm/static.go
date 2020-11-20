// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"github.com/ava-labs/avalanchego/vms/avm/static"
	"github.com/ava-labs/avalanchego/vms/avm/vmargs"
)

func BuildGenesis(args *vmargs.BuildGenesisArgs) (*vmargs.BuildGenesisReply, error) {
	reply := &vmargs.BuildGenesisReply{}
	return reply, static.BuildGenesis(args, reply)
}
