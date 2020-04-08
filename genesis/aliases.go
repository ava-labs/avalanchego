// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/vms/avm"
	"github.com/ava-labs/gecko/vms/evm"
	"github.com/ava-labs/gecko/vms/nftfx"
	"github.com/ava-labs/gecko/vms/platformvm"
	"github.com/ava-labs/gecko/vms/propertyfx"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
	"github.com/ava-labs/gecko/vms/spchainvm"
	"github.com/ava-labs/gecko/vms/spdagvm"
	"github.com/ava-labs/gecko/vms/timestampvm"
)

// Aliases returns the default aliases based on the network ID
func Aliases(networkID uint32) (map[string][]string, map[[32]byte][]string, map[[32]byte][]string, error) {
	generalAliases := map[string][]string{
		"vm/" + platformvm.ID.String():  []string{"vm/platform"},
		"vm/" + avm.ID.String():         []string{"vm/avm"},
		"vm/" + evm.ID.String():         []string{"vm/evm"},
		"vm/" + spdagvm.ID.String():     []string{"vm/spdag"},
		"vm/" + spchainvm.ID.String():   []string{"vm/spchain"},
		"vm/" + timestampvm.ID.String(): []string{"vm/timestamp"},
		"bc/" + ids.Empty.String():      []string{"P", "platform", "bc/P", "bc/platform"},
	}
	chainAliases := map[[32]byte][]string{
		ids.Empty.Key(): []string{"P", "platform"},
	}
	vmAliases := map[[32]byte][]string{
		platformvm.ID.Key():  []string{"platform"},
		avm.ID.Key():         []string{"avm"},
		evm.ID.Key():         []string{"evm"},
		spdagvm.ID.Key():     []string{"spdag"},
		spchainvm.ID.Key():   []string{"spchain"},
		timestampvm.ID.Key(): []string{"timestamp"},
		secp256k1fx.ID.Key(): []string{"secp256k1fx"},
		nftfx.ID.Key():       []string{"nftfx"},
		propertyfx.ID.Key():  []string{"propertyfx"},
	}

	genesisBytes, err := Genesis(networkID)
	if err != nil {
		return nil, nil, nil, err
	}

	genesis := &platformvm.Genesis{} // TODO let's not re-create genesis to do aliasing
	if err := platformvm.Codec.Unmarshal(genesisBytes, genesis); err != nil {
		return nil, nil, nil, err
	}
	if err := genesis.Initialize(); err != nil {
		return nil, nil, nil, err
	}

	for _, chain := range genesis.Chains {
		switch {
		case avm.ID.Equals(chain.VMID):
			generalAliases["bc/"+chain.ID().String()] = []string{"X", "avm", "bc/X", "bc/avm"}
			chainAliases[chain.ID().Key()] = []string{"X", "avm"}
		case evm.ID.Equals(chain.VMID):
			generalAliases["bc/"+chain.ID().String()] = []string{"C", "evm", "bc/C", "bc/evm"}
			chainAliases[chain.ID().Key()] = []string{"C", "evm"}
		case spdagvm.ID.Equals(chain.VMID):
			generalAliases["bc/"+chain.ID().String()] = []string{"bc/spdag"}
			chainAliases[chain.ID().Key()] = []string{"spdag"}
		case spchainvm.ID.Equals(chain.VMID):
			generalAliases["bc/"+chain.ID().String()] = []string{"bc/spchain"}
			chainAliases[chain.ID().Key()] = []string{"spchain"}
		case timestampvm.ID.Equals(chain.VMID):
			generalAliases["bc/"+chain.ID().String()] = []string{"bc/timestamp"}
			chainAliases[chain.ID().Key()] = []string{"timestamp"}
		}
	}
	return generalAliases, chainAliases, vmAliases, nil
}
