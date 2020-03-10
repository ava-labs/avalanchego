// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spchainvm

import (
	"errors"
	"net/http"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/json"
)

var (
	errAccountHasNoValue = errors.New("account has no value")
)

// StaticService defines the static API exposed by the payments vm
type StaticService struct{}

// APIAccount ...
type APIAccount struct {
	Address ids.ShortID `json:"address"`
	Balance json.Uint64 `json:"balance"`
}

// BuildGenesisArgs are arguments for BuildGenesis
type BuildGenesisArgs struct {
	Accounts []APIAccount `json:"accounts"`
}

// BuildGenesisReply is the reply from BuildGenesis
type BuildGenesisReply struct {
	Bytes formatting.CB58 `json:"bytes"`
}

// BuildGenesis returns the UTXOs such that at least one address in [args.Addresses] is
// referenced in the UTXO.
func (*StaticService) BuildGenesis(_ *http.Request, args *BuildGenesisArgs, reply *BuildGenesisReply) error {
	b := Builder{}

	accounts := []Account(nil)
	for _, account := range args.Accounts {
		if account.Balance == 0 {
			return errAccountHasNoValue
		}

		accounts = append(accounts, b.NewAccount(account.Address, 0, uint64(account.Balance)))
	}

	c := Codec{}
	bytes, err := c.MarshalGenesis(accounts)
	reply.Bytes.Bytes = bytes
	return err
}
