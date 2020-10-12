// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package admin

import (
	"net/http"

	"github.com/ava-labs/avalanchego/ids"
)

// GetChainAliasesArgs are the arguments for Admin.GetChainAliases API call
type GetChainAliasesArgs struct{ ChainID string }

// GetChainAliasesReply are the arguments for Admin.GetChainAliases API call
type GetChainAliasesReply struct{ Aliases []string }

// GetChainAliases returns the aliases of the chain
// whose string representation is [args.ChainID]
func (service *Admin) GetChainAliases(r *http.Request, args *GetChainAliasesArgs, reply *GetChainAliasesReply) error {
	ID, err := ids.FromString(args.ChainID)
	if err != nil {
		return err
	}
	reply.Aliases = service.chainManager.Aliases(ID)
	return nil
}
