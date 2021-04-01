package eth

import (
	"context"
	"crypto/rand"
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/coreth/accounts/keystore"

	"github.com/ava-labs/coreth/core/rawdb"

	"github.com/ava-labs/coreth/eth/filters"

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/miner"
	"github.com/ava-labs/coreth/node"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

func TestBlockLogsAllowUnfinalized(t *testing.T) {
	config := DefaultConfig

	chainConfig := &params.ChainConfig{
		ChainID:             big.NewInt(1),
		HomesteadBlock:      big.NewInt(0),
		DAOForkBlock:        big.NewInt(0),
		DAOForkSupport:      true,
		EIP150Block:         big.NewInt(0),
		EIP150Hash:          common.HexToHash("0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0"),
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
		Ethash:              nil,
	}

	// configure the genesis block
	genBalance := big.NewInt(100000000000000000)
	genKey, err := keystore.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	config.Genesis = &core.Genesis{
		Config:     chainConfig,
		Nonce:      0,
		Number:     0,
		ExtraData:  hexutil.MustDecode("0x00"),
		GasLimit:   100000000,
		Difficulty: big.NewInt(0),
		Alloc:      core.GenesisAlloc{genKey.Address: {Balance: genBalance}},
	}

	// grab the control of block generation
	config.Miner.ManualMining = true

	nodecfg := &node.Config{}
	node, err := node.New(nodecfg)
	if err != nil {
		t.Fatal(err)
	}

	baseDB := rawdb.NewMemoryDatabase()

	cb := new(dummy.ConsensusCallbacks)
	mcb := new(miner.MinerCallbacks)
	bcb := new(BackendCallbacks)
	backend, err := New(node, &config, cb, mcb, bcb, baseDB, DefaultSettings)
	if err != nil {
		t.Fatal(err)
	}

	etherBase := &BlackholeAddr
	backend.SetEtherbase(*etherBase)

	ethbackend := EthAPIBackend{eth: backend, gpo: nil}

	api := filters.NewPublicFilterAPI(&ethbackend, true, 10)

	backend.StartMining(0)
	backend.Start()

	backend.BlockChain().UnlockIndexing()
	backend.BlockChain().SetPreference(backend.BlockChain().Genesis())
	backend.BlockChain().Accept(backend.BlockChain().Genesis())

	chainID := chainConfig.ChainID
	nonce := uint64(0)
	gasLimit := 10000000
	gasPrice := big.NewInt(1000000000)

	// *NOTE* this was pre-compiled for the test..
	/*
		pragma solidity >=0.6.0;

		contract Counter {
		    uint256 x;

		    event CounterEmit(uint256 indexed oldval, uint256 indexed newval);

		    constructor() public {
		        emit CounterEmit(0, 42);
		        x = 42;
		    }

		    function add(uint256 y) public returns (uint256) {
		        x = x + y;
		        emit CounterEmit(y, x);
		        return x;
		    }
		}
	*/
	// contracts, err := compiler.CompileSolidityString("", src)
	// checkError(err)
	// contract, _ := contracts[fmt.Sprintf("%s:%s", ".", "Counter")]
	// _ = contract

	// solc-linux-amd64-v0.6.12+commit.27d51765 --bin -o counter.bin counter.sol

	code := common.Hex2Bytes(
		"608060405234801561001057600080fd5b50602a60007f53564ba0be98bdbd40460eb78d2387edab91de6a842e1449053dae1f07439a3160405160405180910390a3602a60008190555060e9806100576000396000f3fe6080604052348015600f57600080fd5b506004361060285760003560e01c80631003e2d214602d575b600080fd5b605660048036036020811015604157600080fd5b8101908080359060200190929190505050606c565b6040518082815260200191505060405180910390f35b60008160005401600081905550600054827f53564ba0be98bdbd40460eb78d2387edab91de6a842e1449053dae1f07439a3160405160405180910390a3600054905091905056fea2646970667358221220dd9c84516cd903bf6a151cbdaef2f2514c28f2f422782a388a2774412b81f08864736f6c634300060c0033",
		// contract.Code[2:],
	)

	tx := types.NewContractCreation(nonce, big.NewInt(0), uint64(gasLimit), gasPrice, code)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), genKey.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	backend.TxPool().AddRemotes([]*types.Transaction{signedTx})
	time.Sleep(time.Second)

	backend.Miner().GenBlock()

	time.Sleep(1 * time.Second)
	var cbx *types.Block
	for icnt := 0; icnt < 10; icnt++ {
		cbx = backend.blockchain.CurrentBlock()
		if cbx.NumberU64() == uint64(1) {
			break
		}
		time.Sleep(1 * time.Second)
	}
	if cbx == nil {
		t.Fatal("block not created")
	}
	cbx = backend.blockchain.CurrentBlock()
	if cbx.NumberU64() != uint64(1) {
		t.Fatal("block not created")
	}

	ctx := context.Background()
	fc := filters.FilterCriteria{
		FromBlock: big.NewInt(1),
	}

	fid, err := api.NewFilter(fc)
	if err != nil {
		t.Fatalf("NewFilter failed %s", err)
	}

	backend.BlockChain().GetVMConfig().AllowUnfinalizedQueries = true
	logs, err := api.GetLogs(ctx, fc)
	if err != nil {
		t.Fatalf("GetLogs failed %s", err)
	}
	if len(logs) != 1 {
		t.Fatalf("GetLogs failed")
	}
	if logs[0].BlockNumber != 1 {
		t.Fatalf("GetLogs failed")
	}

	logs, err = api.GetFilterLogs(ctx, fid)
	if err != nil {
		t.Fatalf("GetLogs failed %s", err)
	}
	if len(logs) != 1 {
		t.Fatalf("GetLogs failed")
	}
	if logs[0].BlockNumber != 1 {
		t.Fatalf("GetLogs failed")
	}

	backend.BlockChain().GetVMConfig().AllowUnfinalizedQueries = false
	logs, err = api.GetLogs(ctx, fc)
	if logs != nil {
		t.Fatalf("GetLogs failed")
	}
	if err == nil || err.Error() != "requested from block 1 after last accepted block 0" {
		t.Fatalf("GetLogs failed %s", err)
	}

	fc2 := filters.FilterCriteria{
		FromBlock: big.NewInt(0),
		ToBlock:   big.NewInt(1),
	}
	logs, err = api.GetLogs(ctx, fc2)
	if logs != nil {
		t.Fatalf("GetLogs failed")
	}
	if err == nil || err.Error() != "requested to block 1 after last accepted block 0" {
		t.Fatalf("GetLogs failed %s", err)
	}

	logs, err = api.GetFilterLogs(ctx, fid)
	if logs != nil {
		t.Fatalf("GetLogs failed")
	}
	if err == nil || err.Error() != "requested from block 1 after last accepted block 0" {
		t.Fatalf("GetLogs failed %s", err)
	}

	fid2, err := api.NewFilter(fc2)
	if err != nil {
		t.Fatalf("NewFilter failed %s", err)
	}
	logs, err = api.GetFilterLogs(ctx, fid2)
	if logs != nil {
		t.Fatalf("GetLogs failed")
	}
	if err == nil || err.Error() != "requested to block 1 after last accepted block 0" {
		t.Fatalf("GetLogs failed %s", err)
	}

	backend.blockchain.Accept(cbx)

	backend.BlockChain().GetVMConfig().AllowUnfinalizedQueries = false
	logs, err = api.GetLogs(ctx, fc)
	if err != nil {
		t.Fatalf("GetLogs failed %s", err)
	}
	if len(logs) != 1 {
		t.Fatalf("GetLogs failed")
	}
	if logs[0].BlockNumber != 1 {
		t.Fatalf("GetLogs failed")
	}

	logs, err = api.GetFilterLogs(ctx, fid)
	if err != nil {
		t.Fatalf("GetLogs failed %s", err)
	}
	if len(logs) != 1 {
		t.Fatalf("GetLogs failed")
	}
	if logs[0].BlockNumber != 1 {
		t.Fatalf("GetLogs failed")
	}

	backend.StopPart()
}
