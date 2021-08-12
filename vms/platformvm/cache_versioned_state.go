// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions"
)

var _ VersionedState = &versionedStateImpl{}

type UTXOGetter interface {
	GetUTXO(utxoID ids.ID) (*avax.UTXO, error)
}

type UTXOAdder interface {
	AddUTXO(utxo *avax.UTXO)
}

type UTXODeleter interface {
	DeleteUTXO(utxoID ids.ID)
}

type UTXOState interface {
	UTXOGetter
	UTXOAdder
	UTXODeleter
}

type MutableState interface {
	UTXOState

	AddRewardUTXO(txID ids.ID, utxo *avax.UTXO)
	GetRewardUTXOs(txID ids.ID) ([]*avax.UTXO, error)

	GetTimestamp() time.Time
	SetTimestamp(time.Time)

	GetCurrentSupply() uint64
	SetCurrentSupply(uint64)

	GetSubnets() ([]*transactions.SignedTx, error)
	AddSubnet(createSubnetTx *transactions.SignedTx)

	GetChains(subnetID ids.ID) ([]*transactions.SignedTx, error)
	AddChain(createChainTx *transactions.SignedTx)

	GetTx(txID ids.ID) (*transactions.SignedTx, Status, error)
	AddTx(tx *transactions.SignedTx, status Status)

	CurrentStakerChainState() currentStakerChainState
	PendingStakerChainState() pendingStakerChainState
}

type VersionedState interface {
	MutableState

	SetBase(MutableState)
	Apply(InternalState)
}

type versionedStateImpl struct {
	parentState MutableState

	currentStakerChainState currentStakerChainState
	pendingStakerChainState pendingStakerChainState

	timestamp time.Time

	currentSupply uint64

	addedSubnets  []*transactions.SignedTx
	cachedSubnets []*transactions.SignedTx

	addedChains  map[ids.ID][]*transactions.SignedTx
	cachedChains map[ids.ID][]*transactions.SignedTx

	// map of txID -> []*UTXO
	addedRewardUTXOs map[ids.ID][]*avax.UTXO

	// map of txID -> {*transactions.Tx, Status}
	addedTxs map[ids.ID]*txStatusImpl

	// map of modified UTXOID -> *UTXO if the UTXO is nil, it has been removed
	modifiedUTXOs map[ids.ID]*utxoImpl
}

type txStatusImpl struct {
	tx     *transactions.SignedTx
	status Status
}

type utxoImpl struct {
	utxoID ids.ID
	utxo   *avax.UTXO
}

func newVersionedState(
	ps MutableState,
	current currentStakerChainState,
	pending pendingStakerChainState,
) VersionedState {
	return &versionedStateImpl{
		parentState:             ps,
		currentStakerChainState: current,
		pendingStakerChainState: pending,
		timestamp:               ps.GetTimestamp(),
		currentSupply:           ps.GetCurrentSupply(),
	}
}

func (vs *versionedStateImpl) GetTimestamp() time.Time {
	return vs.timestamp
}

func (vs *versionedStateImpl) SetTimestamp(timestamp time.Time) {
	vs.timestamp = timestamp
}

func (vs *versionedStateImpl) GetCurrentSupply() uint64 {
	return vs.currentSupply
}

func (vs *versionedStateImpl) SetCurrentSupply(currentSupply uint64) {
	vs.currentSupply = currentSupply
}

func (vs *versionedStateImpl) GetSubnets() ([]*transactions.SignedTx, error) {
	if len(vs.addedSubnets) == 0 {
		return vs.parentState.GetSubnets()
	}
	if len(vs.cachedSubnets) != 0 {
		return vs.cachedSubnets, nil
	}
	subnets, err := vs.parentState.GetSubnets()
	if err != nil {
		return nil, err
	}
	newSubnets := make([]*transactions.SignedTx, len(subnets)+len(vs.addedSubnets))
	copy(newSubnets, subnets)
	for i, subnet := range vs.addedSubnets {
		newSubnets[i+len(subnets)] = subnet
	}
	vs.cachedSubnets = newSubnets
	return newSubnets, nil
}

func (vs *versionedStateImpl) AddSubnet(createSubnetTx *transactions.SignedTx) {
	vs.addedSubnets = append(vs.addedSubnets, createSubnetTx)
	if vs.cachedSubnets != nil {
		vs.cachedSubnets = append(vs.cachedSubnets, createSubnetTx)
	}
}

func (vs *versionedStateImpl) GetChains(subnetID ids.ID) ([]*transactions.SignedTx, error) {
	if len(vs.addedChains) == 0 {
		// No chains have been added
		return vs.parentState.GetChains(subnetID)
	}
	addedChains := vs.addedChains[subnetID]
	if len(addedChains) == 0 {
		// No chains have been added to this subnet
		return vs.parentState.GetChains(subnetID)
	}

	// There have been chains added to the requested subnet

	if vs.cachedChains == nil {
		// This is the first time we are going to be caching the subnet chains
		vs.cachedChains = make(map[ids.ID][]*transactions.SignedTx)
	}

	cachedChains, cached := vs.cachedChains[subnetID]
	if cached {
		return cachedChains, nil
	}

	// This chain wasn't cached yet
	chains, err := vs.parentState.GetChains(subnetID)
	if err != nil {
		return nil, err
	}

	newChains := make([]*transactions.SignedTx, len(chains)+len(addedChains))
	copy(newChains, chains)
	for i, chain := range addedChains {
		newChains[i+len(chains)] = chain
	}
	vs.cachedChains[subnetID] = newChains
	return newChains, nil
}

func (vs *versionedStateImpl) AddChain(createChainTx *transactions.SignedTx) {
	tx := createChainTx.UnsignedTx.(*UnsignedCreateChainTx)
	if vs.addedChains == nil {
		vs.addedChains = map[ids.ID][]*transactions.SignedTx{
			tx.SubnetID: {createChainTx},
		}
	} else {
		vs.addedChains[tx.SubnetID] = append(vs.addedChains[tx.SubnetID], createChainTx)
	}

	cachedChains, cached := vs.cachedChains[tx.SubnetID]
	if !cached {
		return
	}
	vs.cachedChains[tx.SubnetID] = append(cachedChains, createChainTx)
}

func (vs *versionedStateImpl) GetTx(txID ids.ID) (*transactions.SignedTx, Status, error) {
	tx, exists := vs.addedTxs[txID]
	if !exists {
		return vs.parentState.GetTx(txID)
	}
	return tx.tx, tx.status, nil
}

func (vs *versionedStateImpl) AddTx(tx *transactions.SignedTx, status Status) {
	txID := tx.ID()
	txStatus := &txStatusImpl{
		tx:     tx,
		status: status,
	}
	if vs.addedTxs == nil {
		vs.addedTxs = map[ids.ID]*txStatusImpl{
			txID: txStatus,
		}
	} else {
		vs.addedTxs[txID] = txStatus
	}
}

func (vs *versionedStateImpl) GetRewardUTXOs(txID ids.ID) ([]*avax.UTXO, error) {
	if utxos, exists := vs.addedRewardUTXOs[txID]; exists {
		return utxos, nil
	}
	return vs.parentState.GetRewardUTXOs(txID)
}

func (vs *versionedStateImpl) AddRewardUTXO(txID ids.ID, utxo *avax.UTXO) {
	if vs.addedRewardUTXOs == nil {
		vs.addedRewardUTXOs = make(map[ids.ID][]*avax.UTXO)
	}
	vs.addedRewardUTXOs[txID] = append(vs.addedRewardUTXOs[txID], utxo)
}

func (vs *versionedStateImpl) GetUTXO(utxoID ids.ID) (*avax.UTXO, error) {
	utxo, modified := vs.modifiedUTXOs[utxoID]
	if !modified {
		return vs.parentState.GetUTXO(utxoID)
	}
	if utxo.utxo == nil {
		return nil, database.ErrNotFound
	}
	return utxo.utxo, nil
}

func (vs *versionedStateImpl) AddUTXO(utxo *avax.UTXO) {
	newUTXO := &utxoImpl{
		utxoID: utxo.InputID(),
		utxo:   utxo,
	}
	if vs.modifiedUTXOs == nil {
		vs.modifiedUTXOs = map[ids.ID]*utxoImpl{
			utxo.InputID(): newUTXO,
		}
	} else {
		vs.modifiedUTXOs[utxo.InputID()] = newUTXO
	}
}

func (vs *versionedStateImpl) DeleteUTXO(utxoID ids.ID) {
	newUTXO := &utxoImpl{
		utxoID: utxoID,
	}
	if vs.modifiedUTXOs == nil {
		vs.modifiedUTXOs = map[ids.ID]*utxoImpl{
			utxoID: newUTXO,
		}
	} else {
		vs.modifiedUTXOs[utxoID] = newUTXO
	}
}

func (vs *versionedStateImpl) CurrentStakerChainState() currentStakerChainState {
	return vs.currentStakerChainState
}

func (vs *versionedStateImpl) PendingStakerChainState() pendingStakerChainState {
	return vs.pendingStakerChainState
}

func (vs *versionedStateImpl) SetBase(parentState MutableState) {
	vs.parentState = parentState
}

func (vs *versionedStateImpl) Apply(is InternalState) {
	is.SetTimestamp(vs.timestamp)
	is.SetCurrentSupply(vs.currentSupply)
	for _, subnet := range vs.addedSubnets {
		is.AddSubnet(subnet)
	}
	for _, chains := range vs.addedChains {
		for _, chain := range chains {
			is.AddChain(chain)
		}
	}
	for _, tx := range vs.addedTxs {
		is.AddTx(tx.tx, tx.status)
	}
	for txID, utxos := range vs.addedRewardUTXOs {
		for _, utxo := range utxos {
			is.AddRewardUTXO(txID, utxo)
		}
	}
	for _, utxo := range vs.modifiedUTXOs {
		if utxo.utxo != nil {
			is.AddUTXO(utxo.utxo)
		} else {
			is.DeleteUTXO(utxo.utxoID)
		}
	}
	vs.currentStakerChainState.Apply(is)
	vs.pendingStakerChainState.Apply(is)
}
