package evm

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/chain"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func getTheValidTx(vm *VM, sharedMemory *atomic.Memory, t *testing.T) *Tx {
	importAmount := uint64(50000000)
	utxoID := avax.UTXOID{
		TxID: ids.ID{
			0x0f, 0x2f, 0x4f, 0x6f, 0x8e, 0xae, 0xce, 0xee,
			0x0d, 0x2d, 0x4d, 0x6d, 0x8c, 0xac, 0xcc, 0xec,
			0x0b, 0x2b, 0x4b, 0x6b, 0x8a, 0xaa, 0xca, 0xea,
			0x09, 0x29, 0x49, 0x69, 0x88, 0xa8, 0xc8, 0xe8,
		},
	}

	utxo := &avax.UTXO{
		UTXOID: utxoID,
		Asset:  avax.Asset{ID: vm.ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: importAmount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{testKeys[0].PublicKey().Address()},
			},
		},
	}
	utxoBytes, err := vm.codec.Marshal(codecVersion, utxo)
	if err != nil {
		t.Fatal(err)
	}

	xChainSharedMemory := sharedMemory.NewSharedMemory(vm.ctx.XChainID)
	inputID := utxo.InputID()
	if err := xChainSharedMemory.Apply(map[ids.ID]*atomic.Requests{vm.ctx.ChainID: {PutRequests: []*atomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			testKeys[0].PublicKey().Address().Bytes(),
		},
	}}}}); err != nil {
		t.Fatal(err)
	}

	importTx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[0], []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	return importTx
}

func getTheIllFormedTx(vm *VM, sharedMemory *atomic.Memory, t *testing.T) *Tx {
	importAmount := uint64(50000000)
	utxoID := avax.UTXOID{
		TxID: ids.ID{
			0x0f, 0x2f, 0x4f, 0x6f, 0x8e, 0xae, 0xce, 0xee,
			0x0d, 0x2d, 0x4d, 0x6d, 0x8c, 0xac, 0xcc, 0xec,
			0x0b, 0x2b, 0x4b, 0x6b, 0x8a, 0xaa, 0xca, 0xea,
			0x09, 0x29, 0x49, 0x69, 0x88, 0xa8, 0xc8, 0xe8,
		},
	}

	utxo := &avax.UTXO{
		UTXOID: utxoID,
		Asset:  avax.Asset{ID: vm.ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: importAmount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{testKeys[0].PublicKey().Address()},
			},
		},
	}
	utxoBytes, err := vm.codec.Marshal(codecVersion, utxo)
	if err != nil {
		t.Fatal(err)
	}

	xChainSharedMemory := sharedMemory.NewSharedMemory(vm.ctx.XChainID)
	inputID := utxo.InputID()
	if err := xChainSharedMemory.Apply(map[ids.ID]*atomic.Requests{vm.ctx.ChainID: {PutRequests: []*atomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			testKeys[0].PublicKey().Address().Bytes(),
		},
	}}}}); err != nil {
		t.Fatal(err)
	}

	// code below extracted from newImportTx to make an invalidTx
	kc := secp256k1fx.NewKeychain()
	kc.Add(testKeys[0])

	atomicUTXOs, _, _, err := vm.GetAtomicUTXOs(vm.ctx.XChainID, kc.Addresses(),
		ids.ShortEmpty, ids.Empty, -1)
	if err != nil {
		t.Fatal(err)
	}

	importedInputs := []*avax.TransferableInput{}
	signers := [][]*crypto.PrivateKeySECP256K1R{}

	importedAmount := make(map[ids.ID]uint64)
	now := vm.clock.Unix()
	for _, utxo := range atomicUTXOs {
		inputIntf, utxoSigners, err := kc.Spend(utxo.Out, now)
		if err != nil {
			continue
		}
		input, ok := inputIntf.(avax.TransferableIn)
		if !ok {
			continue
		}
		aid := utxo.AssetID()
		importedAmount[aid], err = math.Add64(importedAmount[aid], input.Amount())
		if err != nil {
			t.Fatal(err)
		}
		importedInputs = append(importedInputs, &avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  utxo.Asset,
			In:     input,
		})
		signers = append(signers, utxoSigners)
	}
	avax.SortTransferableInputsWithSigners(importedInputs, signers)
	importedAVAXAmount := importedAmount[vm.ctx.AVAXAssetID]
	outs := []EVMOutput{}

	// AVAX output
	if importedAVAXAmount < vm.txFee { // imported amount goes toward paying tx fee
		t.Fatal(errInsufficientFundsForFee)
	} else if importedAVAXAmount > vm.txFee {
		outs = append(outs, EVMOutput{
			Address: testEthAddrs[0],
			Amount:  importedAVAXAmount - vm.txFee,
			AssetID: vm.ctx.AVAXAssetID,
		})
	}

	// This will create unique outputs (in the context of sorting)
	// since each output will have a unique assetID
	for assetID, amount := range importedAmount {
		// Skip the AVAX amount since it has already been included
		// and skip any input with an amount of 0
		if assetID == vm.ctx.AVAXAssetID || amount == 0 {
			continue
		}
		outs = append(outs, EVMOutput{
			Address: testEthAddrs[0],
			Amount:  amount,
			AssetID: assetID,
		})
	}

	// If no outputs are produced, return an error.
	// Note: this can happen if there is exactly enough AVAX to pay the
	// transaction fee, but no other funds to be imported.
	if len(outs) == 0 {
		t.Fatal(errNoEVMOutputs)
	}

	SortEVMOutputs(outs)

	// Create the transaction
	utx := &UnsignedImportTx{
		NetworkID:      vm.ctx.NetworkID,
		BlockchainID:   vm.ctx.ChainID,
		Outs:           outs,
		ImportedInputs: importedInputs,
		SourceChain:    ids.ID{'f', 'a', 'k', 'e'}, // This should make the tx invalid
	}
	tx := &Tx{UnsignedAtomicTx: utx}
	if err := tx.Sign(vm.codec, signers); err != nil {
		t.Fatal(err)
	}
	return tx
}

func TestMempool_Add_Gossiped_CreateChainTx(t *testing.T) {
	// shows that a CreateChainTx received as gossip response can be added to mempool
	// and then remove by inclusion in a block

	issuer, vm, _, sharedMemory, _ := GenesisVM(t, true, genesisJSONApricotPhase0, "", "")
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	mempool := vm.mempool

	// create tx to be gossiped
	tx := getTheValidTx(vm, sharedMemory, t)

	// gossip tx and check it is accepted
	nodeID := ids.ShortID{'n', 'o', 'd', 'e'}
	if err := vm.AppResponse(nodeID, vm.IssueID(), tx.Bytes()); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	<-issuer

	if !mempool.has(tx.ID()) {
		t.Fatal("Issued tx not recorded into mempool")
	}

	// show that build block include that tx and tx is still in mempool
	blk, err := vm.BuildBlock()
	if err != nil {
		t.Fatal("could not build block out of mempool")
	}

	evmBlk, ok := blk.(*chain.BlockWrapper).Block.(*Block)
	if !ok {
		t.Fatal("expected standard block")
	}
	retrievedTx, err := vm.extractAtomicTx(evmBlk.ethBlock)
	if err != nil {
		t.Fatal("could not extract atomic tx")
	}
	if retrievedTx.ID() != tx.ID() {
		t.Fatal("standard block does not include expected transaction")
	}

	if !mempool.has(tx.ID()) {
		t.Fatal("tx should stay in mempool till block is accepted")
	}

	// TODO: unlock Accept Block
	// // show that once block is accepted, it removes tx from mempool
	// if err := blk.Accept(); err != nil {
	// 	t.Fatal("could not accept block")
	// }

	// if mempool.has(tx.ID()) {
	// 	t.Fatal("tx included in block is still recorded into mempool")
	// }
}

func TestMempool_Add_LocallyCreate_CreateChainTx(t *testing.T) {
	// shows that a locally generated CreateChainTx can be added to mempool
	// and then removed by inclusion in a block

	issuer, vm, _, sharedMemory, _ := GenesisVM(t, true, genesisJSONApricotPhase0, "", "")
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	mempool := vm.mempool

	// add a tx to it
	tx := getTheValidTx(vm, sharedMemory, t)
	if err := vm.issueTx(tx); err != nil {
		t.Fatal("Could not add tx to mempool")
	}
	<-issuer

	if !mempool.has(tx.ID()) {
		t.Fatal("Issued tx not recorded into mempool")
	}

	// show that build block include that tx and tx is still in mempool
	blk, err := vm.BuildBlock()
	if err != nil {
		t.Fatal("could not build block out of mempool")
	}

	evmBlk, ok := blk.(*chain.BlockWrapper).Block.(*Block)
	if !ok {
		t.Fatal("expected standard block")
	}
	retrievedTx, err := vm.extractAtomicTx(evmBlk.ethBlock)
	if err != nil {
		t.Fatal("could not extract atomic tx")
	}
	if retrievedTx.ID() != tx.ID() {
		t.Fatal("standard block does not include expected transaction")
	}

	if !mempool.has(tx.ID()) {
		t.Fatal("tx should stay in mempool till block is accepted")
	}

	// TODO: unlock Accept Block
	// // show that once block is accepted, it removes tx from mempool
	// if err := blk.Accept(); err != nil {
	// 	t.Fatal("could not accept block")
	// }

	// if mempool.has(tx.ID()) {
	// 	t.Fatal("tx included in block is still recorded into mempool")
	// }
}

// func TestMempool_MaxMempoolSizeHandling(t *testing.T) {
// 	// shows that valid tx is not added to mempool if this would exceed its maximum size

// 	_, vm, _, sharedMemory, _ := GenesisVM(t, true, genesisJSONApricotPhase0, "", "")
// 	defer func() {
// 		if err := vm.Shutdown(); err != nil {
// 			t.Fatal(err)
// 		}
// 	}()
// vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
// 	mempool := vm.mempool

// 	// create candidate tx
// 	tx := getValidTx(vm, sharedMemory, t)

// 	// shortcut to simulated almost filled mempool
// 	// TODO: does not work with mempool as is now. Figure out a way to test it
// 	mempool.maxSize = defaultMempoolSize - len(tx.Bytes()) + 1

// 	if err := mempool.AddTx(tx); err != errTooManyAtomicTx {
// 		t.Fatal("max mempool size breached")
// 	}

// 	// shortcut to simulated almost filled mempool
// 	mempool.maxSize = defaultMempoolSize - len(tx.Bytes())

// 	if err := mempool.AddTx(tx); err != nil {
// 		t.Fatal("should be possible to add tx")
// 	}
// }

func TestMempool_AppResponseHandling(t *testing.T) {
	// show that a tx discovered by a GossipResponse is re-gossiped
	// only if duly added to mempool

	_, vm, _, sharedMemory, sender := GenesisVM(t, true, genesisJSONApricotPhase0, "", "")
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	mempool := vm.mempool

	isTxReGossiped := false
	var gossipedBytes []byte
	sender.CantSendAppGossip = true
	sender.SendAppGossipF = func(b []byte) error {
		isTxReGossiped = true
		gossipedBytes = b
		return nil
	}

	// create tx to be received from AppGossipResponse
	tx := getTheValidTx(vm, sharedMemory, t)

	// responses with unknown requestID are rejected
	nodeID := ids.ShortID{'n', 'o', 'd', 'e'}
	reqID := vm.IssueID()

	unknownReqID := reqID + 1
	if err := vm.AppResponse(nodeID, unknownReqID, tx.Bytes()); err != nil {
		t.Fatal("responses with unknown requestID should be dropped")
	}
	if mempool.has(tx.ID()) {
		t.Fatal("responses with unknown requestID should not affect mempool")
	}
	if isTxReGossiped {
		t.Fatal("responses with unknown requestID should not result in gossiping")
	}

	// received tx and check it is accepted and re-gossiped
	if err := vm.AppResponse(nodeID, reqID, tx.Bytes()); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if !mempool.has(tx.ID()) {
		t.Fatal("Issued tx not recorded into mempool")
	}
	if !isTxReGossiped {
		t.Fatal("tx accepted in mempool should have been re-gossiped")
	}

	// show that gossiped bytes can be duly decoded
	if err := vm.AppGossip(nodeID, gossipedBytes); err != nil {
		t.Fatal("bytes gossiped following AppResponse are not valid")
	}

	// show that if tx is not accepted to mempool is not regossiped

	// case 1: reinsertion attempt
	isTxReGossiped = false
	if err := vm.AppResponse(nodeID, vm.IssueID(), tx.Bytes()); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if isTxReGossiped {
		t.Fatal("unaccepted tx should have not been regossiped")
	}
}

func TestMempool_AppResponseHandling_InvalidTx(t *testing.T) {
	// show that invalid txes are not accepted to mempool, nor rejected

	_, vm, _, sharedMemory, sender := GenesisVM(t, true, genesisJSONApricotPhase0, "", "")
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	mempool := vm.mempool

	isTxReGossiped := false
	sender.CantSendAppGossip = true
	sender.SendAppGossipF = func([]byte) error {
		isTxReGossiped = true
		return nil
	}

	// create an invalid tx
	illFormedTx := getTheIllFormedTx(vm, sharedMemory, t)

	// gossip tx and check it is accepted and re-gossiped
	nodeID := ids.ShortID{'n', 'o', 'd', 'e'}
	reqID := vm.IssueID()
	if err := vm.AppResponse(nodeID, reqID, illFormedTx.Bytes()); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if mempool.has(illFormedTx.ID()) {
		t.Fatal("invalid tx should not be accepted to mempool")
	}
	if isTxReGossiped {
		t.Fatal("invalid tx should not be re-gossiped")
	}
	if !mempool.isRejected(illFormedTx.ID()) {
		t.Fatal("invalid tx should be marked as rejected")
	}
}

func TestMempool_AppGossipHandling(t *testing.T) {
	// show that a txID discovered from gossip is requested to the same node
	// only if the txID is unknown

	_, vm, _, sharedMemory, sender := GenesisVM(t, true, genesisJSONApricotPhase0, "", "")
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	mempool := vm.mempool

	isTxRequested := false
	nodeID := ids.ShortID{'n', 'o', 'd', 'e'}
	IsRightNodeRequested := false
	var requestedBytes []byte
	sender.CantSendAppRequest = true
	sender.SendAppRequestF = func(nodes ids.ShortSet, reqID uint32, resp []byte) error {
		isTxRequested = true
		if nodes.Contains(nodeID) {
			IsRightNodeRequested = true
		}
		requestedBytes = resp
		return nil
	}

	// create a tx
	tx := getTheValidTx(vm, sharedMemory, t)
	txID, err := vm.codec.Marshal(codecVersion, tx.ID())
	if err != nil {
		t.Fatal(err)
	}

	// show that unknown txID is requested
	if err := vm.AppGossip(nodeID, txID); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if !isTxRequested {
		t.Fatal("unknown txID should have been requested")
	}
	if !IsRightNodeRequested {
		t.Fatal("unknown txID should have been requested to a different node")
	}

	// show that requested bytes can be duly decoded
	if err := vm.AppRequest(nodeID, vm.IssueID(), requestedBytes); err != nil {
		t.Fatal("requested bytes following gossiping cannot be decoded")
	}

	// show that known txID is not requested
	isTxRequested = false
	if err := mempool.AddTx(tx); err != nil {
		t.Fatal("could not add tx to mempool")
	}

	if err := vm.AppGossip(nodeID, txID); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if isTxRequested {
		t.Fatal("known txID should not be requested")
	}
}

func TestMempool_AppGossipHandling_InvalidTx(t *testing.T) {
	// show that txes already marked as invalid are not re-requested on gossiping

	_, vm, _, sharedMemory, sender := GenesisVM(t, true, genesisJSONApricotPhase0, "", "")
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	mempool := vm.mempool

	isTxRequested := false
	sender.CantSendAppRequest = true
	sender.SendAppRequestF = func(ids.ShortSet, uint32, []byte) error {
		isTxRequested = true
		return nil
	}

	// create a tx and mark as invalid
	rejectedTx := getTheValidTx(vm, sharedMemory, t)
	mempool.AddTx(rejectedTx)
	mempool.NextTx()
	mempool.DiscardCurrentTx()
	if !mempool.isRejected(rejectedTx.ID()) {
		t.Fatal("test requires tx to be invalid")
	}

	// show that the invalid tx is not requested
	nodeID := ids.ShortID{'n', 'o', 'd', 'e'}
	rejectedTxID, err := vm.codec.Marshal(codecVersion, rejectedTx.ID())
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.AppGossip(nodeID, rejectedTxID); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if isTxRequested {
		t.Fatal("rejected txs should not be requested")
	}
}

func TestMempool_AppRequestHandling(t *testing.T) {
	// show that a node answer to request with response
	// only if it has the requested tx

	_, vm, _, sharedMemory, sender := GenesisVM(t, true, genesisJSONApricotPhase0, "", "")
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	mempool := vm.mempool

	isResponseIssued := false
	var respondedBytes []byte
	sender.CantSendAppResponse = true
	sender.SendAppResponseF = func(nodeID ids.ShortID, reqID uint32, resp []byte) error {
		isResponseIssued = true
		respondedBytes = resp
		return nil
	}

	// create a tx
	tx := getTheValidTx(vm, sharedMemory, t)
	txID, err := vm.codec.Marshal(codecVersion, tx.ID())
	if err != nil {
		t.Fatal(err)
	}

	// show that there is no response if tx is unknown
	nodeID := ids.ShortID{'n', 'o', 'd', 'e'}
	if err := vm.AppRequest(nodeID, vm.IssueID(), txID); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if isResponseIssued {
		t.Fatal("there should be no response with unknown tx")
	}

	// show that there is response if tx is unknown
	if err := mempool.AddTx(tx); err != nil {
		t.Fatal("could not add tx to mempool")
	}

	if err := vm.AppRequest(nodeID, vm.IssueID(), txID); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if !isResponseIssued {
		t.Fatal("there should be a response with known tx")
	}

	// show that responded bytes can be duly decoded
	if err := vm.AppResponse(nodeID, vm.IssueID(), respondedBytes); err != nil {
		t.Fatal("bytes sent in response of AppRequest cannot be decoded")
	}
}

func TestMempool_AppRequestHandling_InvalidTx(t *testing.T) {
	// should a node issue a request for rejecte tx
	// (which should not have been gossiped around in the first place)
	// no response is sent

	_, vm, _, sharedMemory, sender := GenesisVM(t, true, genesisJSONApricotPhase0, "", "")
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping

	isResponseIssued := false
	sender.CantSendAppResponse = true
	sender.SendAppResponseF = func(ids.ShortID, uint32, []byte) error {
		isResponseIssued = true
		return nil
	}

	// create a tx
	rejectedTx := getTheValidTx(vm, sharedMemory, t)
	rejectedTxID, err := vm.codec.Marshal(codecVersion, rejectedTx.ID())
	if err != nil {
		t.Fatal(err)
	}

	// show that there is no response if tx is rejected
	nodeID := ids.ShortID{'n', 'o', 'd', 'e'}
	if err := vm.AppRequest(nodeID, vm.IssueID(), rejectedTxID); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
	if isResponseIssued {
		t.Fatal("there should be no response with unknown tx")
	}
}

func TestMempool_IssueTxAndGossiping(t *testing.T) {
	// show that locally generated txes are gossiped
	_, vm, _, sharedMemory, sender := GenesisVM(t, true, genesisJSONApricotPhase0, "", "")
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping

	gossipedBytes := make([]byte, 0)
	sender.CantSendAppGossip = true
	sender.SendAppGossipF = func(b []byte) error {
		gossipedBytes = b
		return nil
	}

	// add a tx to it
	tx := getTheValidTx(vm, sharedMemory, t)
	if err := vm.issueTx(tx); err != nil {
		t.Fatal("Could not add tx to mempool")
	}
	if len(gossipedBytes) == 0 {
		t.Fatal("expected call to SendAppGossip not issued")
	}

	// check that gossiped bytes can be requested
	nodeID := ids.ShortID{'n', 'o', 'd', 'e'}
	if err := vm.AppGossip(nodeID, gossipedBytes); err != nil {
		t.Fatal("error in reception of gossiped tx")
	}
}
