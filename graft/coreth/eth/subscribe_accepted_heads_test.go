package eth

import (
	"crypto/ecdsa"
	"crypto/rand"
	"io"
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/miner"
	"github.com/ava-labs/coreth/node"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

var (
	BlackholeAddr = common.Address{
		1, 0, 0, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	}
)

type Key struct {
	Address    common.Address
	PrivateKey *ecdsa.PrivateKey
}

func NewKeyFromECDSA(privateKeyECDSA *ecdsa.PrivateKey) *Key {
	key := &Key{
		Address:    crypto.PubkeyToAddress(privateKeyECDSA.PublicKey),
		PrivateKey: privateKeyECDSA,
	}
	return key
}

func NewKey(rand io.Reader) (*Key, error) {
	privateKeyECDSA, err := ecdsa.GenerateKey(crypto.S256(), rand)
	if err != nil {
		return nil, err
	}
	return NewKeyFromECDSA(privateKeyECDSA), nil
}

func TestAcceptedHeadSubscriptions(t *testing.T) {
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
	genKey, err := NewKey(rand.Reader)
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
	cb := new(dummy.ConsensusCallbacks)
	mcb := new(miner.MinerCallbacks)
	bcb := new(BackendCallbacks)
	backend, err := New(node, &config, cb, mcb, bcb, nil, DefaultSettings)
	if err != nil {
		t.Fatal(err)
	}

	etherBase := &BlackholeAddr
	backend.SetEtherbase(*etherBase)

	ethbackend := EthAPIBackend{eth: backend, gpo: nil}

	acceptedChainCh := make(chan core.ChainEvent, 1000)
	chainCh := make(chan core.ChainEvent, 1000)

	ethbackend.SubscribeChainAcceptedEvent(acceptedChainCh)
	ethbackend.SubscribeChainEvent(chainCh)

	backend.StartMining(0)
	backend.Start()

	backend.BlockChain().UnlockIndexing()
	backend.BlockChain().SetPreference(backend.BlockChain().Genesis())

	chainID := chainConfig.ChainID
	nonce := uint64(0)
	gasLimit := 10000000
	gasPrice := big.NewInt(1000000000)

	// *NOTE* this was pre-compiled for the test..
	// src := `pragma solidity >=0.6.0;
	//
	// contract Counter {
	//     uint256 x;
	//
	//     constructor() public {
	//         x = 42;
	//     }
	//
	//     function add(uint256 y) public returns (uint256) {
	//         x = x + y;
	//         return x;
	//     }
	// }`
	// contracts, err := compiler.CompileSolidityString("", src)
	// checkError(err)
	// contract, _ := contracts[fmt.Sprintf("%s:%s", ".", "Counter")]
	// _ = contract

	// solc-linux-amd64-v0.6.12+commit.27d51765 --bin -o counter.bin counter.sol

	code := common.Hex2Bytes(
		"6080604052348015600f57600080fd5b50602a60008190555060b9806100266000396000f3fe6080604052348015600f57600080fd5b506004361060285760003560e01c80631003e2d214602d575b600080fd5b605660048036036020811015604157600080fd5b8101908080359060200190929190505050606c565b6040518082815260200191505060405180910390f35b60008160005401600081905550600054905091905056fea26469706673582212200dc7c76677426e8c621c6839348a7c8d60787c546a9b9c7fc91efa57f71d46a364736f6c634300060c0033",
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
		if cbx.NumberU64() == 1 {
			break
		}
		time.Sleep(1 * time.Second)
	}
	if cbx == nil {
		t.Fatal("block not created")
	}
	cbx = backend.blockchain.CurrentBlock()
	if cbx.NumberU64() != 1 {
		t.Fatal("block not created")
	}

	select {
	case fb := <-chainCh:
		if fb.Block.NumberU64() != 1 {
			t.Fatal("block not created")
		}
	default:
		t.Fatal("block not created")
	}

	backend.blockchain.Accept(cbx)

	backend.StopPart()

	select {
	case fb := <-acceptedChainCh:
		if fb.Block.NumberU64() != 1 {
			t.Fatal("block not created")
		}
	default:
		t.Fatal("block not created")
	}
}
