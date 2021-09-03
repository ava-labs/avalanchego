package evm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

type appMsgType byte

const (
	// Enum specifying the content of app gossiped messages
	atomicTxID appMsgType = iota + 1
	atomicTx
	ethTxsData
	ethTxList
)

// Information about an Ethereum transaction
// for gossiping and lightweight validation
type EthTxData struct {
	// The transaction's hash
	Hash common.Hash

	// The transaction's sender
	Sender common.Address

	// The transaction's nonce
	Nonce uint64
}

type appMsg struct {
	ContentType appMsgType `serialize:"true"`
	Bytes       []byte     `serialize:"true"`

	// Must only be accessed if [ContentType] == atomicTxID
	txID ids.ID

	// Must only be accessed if [ContentType] == atomicTx
	tx *Tx

	// Must only be accessed if [ContentType] == ethTxsData
	ethTxData []EthTxData

	// Must only be accessed if [ContentType] == ethTxList
	txs []*types.Transaction
}

func encodeAtmData(c codec.Manager, txID ids.ID) ([]byte, error) {
	am := &appMsg{
		ContentType: atomicTxID,
		Bytes:       txID[:],
	}

	return c.Marshal(codecVersion, am)
}

func encodeAtmTx(c codec.Manager, tx *Tx) ([]byte, error) {
	bytes, err := c.Marshal(codecVersion, tx)
	if err != nil {
		return nil, err
	}

	am := &appMsg{
		ContentType: atomicTx,
		Bytes:       bytes,
	}

	return c.Marshal(codecVersion, am)
}

func encodeEthData(c codec.Manager, data []EthTxData) ([]byte, error) {
	if len(data) > maxGossipEthDataSize {
		return nil, fmt.Errorf("got %d ethTxData but expected at most %d", len(data), maxGossipEthDataSize)
	}

	bytes, err := rlp.EncodeToBytes(data)
	if err != nil {
		return nil, err
	}

	am := &appMsg{
		ContentType: ethTxsData,
		Bytes:       bytes,
	}

	return c.Marshal(codecVersion, am)
}

func encodeEthTxs(c codec.Manager, ethTxs []*types.Transaction) ([]byte, error) {
	if len(ethTxs) > maxGossipEthTxsSize {
		return nil, fmt.Errorf("got %d eth txs but expected at most %d", len(ethTxs), maxGossipEthTxsSize)
	}

	bytes, err := rlp.EncodeToBytes(ethTxs)
	if err != nil {
		return nil, err
	}

	am := &appMsg{
		ContentType: ethTxList,
		Bytes:       bytes,
	}

	return c.Marshal(codecVersion, am)
}

func decodeToAppMsg(c codec.Manager, bytes []byte) (*appMsg, error) {
	am := &appMsg{}
	if _, err := c.Unmarshal(bytes, am); err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("could not decode AppMsg, error %v", err))
	}

	switch am.ContentType {
	case atomicTxID:
		txID, err := ids.ToID(am.Bytes)
		if err != nil {
			return nil, fmt.Errorf("TxID bytes cannot be decoded into txID, error %v", err)
		}
		am.txID = txID
		return am, nil

	case atomicTx:
		tx := &Tx{}
		if _, err := c.Unmarshal(am.Bytes, tx); err != nil {
			return nil, fmt.Errorf("could not decode atomic tx, error %v", err)
		}
		unsignedBytes, err := c.Marshal(codecVersion, &tx.UnsignedAtomicTx)
		if err != nil {
			return nil, fmt.Errorf("could not decode unsigned atomic tx, error %v", err)
		}
		tx.Initialize(unsignedBytes, am.Bytes)
		am.tx = tx
		return am, nil

	case ethTxsData:
		dataList := make([]EthTxData, 0)
		if err := rlp.DecodeBytes(am.Bytes, &dataList); err != nil {
			return nil, fmt.Errorf("could not decode AppMsg carrying eth hashes, error %v", err)
		}
		if len(dataList) > maxGossipEthDataSize {
			return nil, fmt.Errorf("got %d ethTxData but expected at most %d", len(dataList), maxGossipEthDataSize)
		}
		am.ethTxData = dataList
		return am, nil

	case ethTxList:
		ethTxs := make([]*types.Transaction, 0)
		if err := rlp.DecodeBytes(am.Bytes, &ethTxs); err != nil {
			return nil, fmt.Errorf("could not decode AppMsg carrying eth txs, error %v", err)
		}
		if len(ethTxs) > maxGossipEthTxsSize {
			return nil, fmt.Errorf("got %d eth txs but expected at most %d", len(ethTxs), maxGossipEthTxsSize)
		}
		am.txs = ethTxs
		return am, nil

	default:
		return nil, fmt.Errorf("unknown AppMsg type: %d", am.ContentType)
	}
}
