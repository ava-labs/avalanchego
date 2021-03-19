// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

// var (
// 	validatorsPrefix = []byte("validators")
// 	blocksPrefix     = []byte("blocks")
// 	txsPrefix        = []byte("txs")
// 	utxosPrefix      = []byte("utxos")
// 	addressesPrefix  = []byte("addresses")
// 	subnetsPrefix    = []byte("subnets")
// 	chainsPrefix     = []byte("chains")
// 	singletonsPrefix = []byte("singletons")

// 	timestampKey     = []byte("timestamp")
// 	currentSupplyKey = []byte("current supply")
// )

// /*
//  * VMDB
//  * |-. validators
//  * | '-. list
//  * |   '-- txID -> validator tx bytes + other metadata
//  * |-. blocks
//  * | '-- blockID -> block bytes
//  * |-. txs
//  * | '-- txID -> blockID that the tx was accepted in
//  * |- utxos
//  * |- addresses
//  * |- subnets
//  * |- chains
//  * '- singletons
//  */
// type cache struct {
// 	validatorDB  linkeddb.LinkedDB
// 	blockDB      database.Database
// 	txDB         database.Database
// 	utxoDB       database.Database
// 	addressDB    database.Database
// 	subnetDB     linkeddb.LinkedDB
// 	blockchainDB database.Database
// 	singletonDB  database.Database
// }

type totalState interface {
	GetTimestamp() time.Time
	SetTimestamp(time.Time)

	GetCurrentSupply() uint64
	SetCurrentSupply(uint64)

	GetSubnets() ([]*Tx, error)
	GetSubnet(subnetID ids.ID) (*Tx, error)
	AddSubnet(createSubnetTx *Tx)

	GetChains(subnetID ids.ID) ([]*Tx, error)
	GetChain(subnetID, chainID ids.ID) (*Tx, error)
	AddChain(createChainTx *Tx)

	GetUTXO(utxoID avax.UTXOID) (*avax.UTXO, error)
	DeleteUTXO(utxoID avax.UTXOID)
	AddUTXO(utxo *avax.UTXO)
}
