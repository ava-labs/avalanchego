// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"flag"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/spf13/viper"
)

const (
	ValidatorBondAmountKey   = "validator-bond-amount"
	DaoProposalBondAmountKey = "dao-proposal-bond-amount"
)

func addCaminoFlags(fs *flag.FlagSet) {
	// Bond amount required to place a DAO proposal on the Primary Network
	fs.Uint64(DaoProposalBondAmountKey, genesis.LocalParams.CaminoConfig.DaoProposalBondAmount, "Amount, in nAVAX, required to place a DAO proposal")
}

func getCaminoPlatformConfig(v *viper.Viper) config.CaminoConfig {
	conf := config.CaminoConfig{
		DaoProposalBondAmount: v.GetUint64(DaoProposalBondAmountKey),
	}
	return conf
}
