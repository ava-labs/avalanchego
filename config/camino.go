// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/caminoconfig"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	DACProposalBondAmountKey = "dac-proposal-bond-amount"
)

func addCaminoFlags(fs *pflag.FlagSet) {
	// Bond amount required to place a DAC proposal on the Primary Network
	fs.Uint64(DACProposalBondAmountKey, genesis.LocalParams.CaminoConfig.DACProposalBondAmount, "Amount, in nAVAX, required to place a DAC proposal")
}

func getCaminoPlatformConfig(v *viper.Viper) caminoconfig.Config {
	conf := caminoconfig.Config{
		DACProposalBondAmount: v.GetUint64(DACProposalBondAmountKey),
	}
	return conf
}
