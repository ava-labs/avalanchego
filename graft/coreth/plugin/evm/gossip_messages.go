package evm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

type appMsgType byte

const (
	atmDataType appMsgType = iota + 1
	atmTxType
	ethDataType
	ethTxsType
)

type EthData struct {
	TxHash    common.Hash
	TxAddress common.Address
	TxNonce   uint64
}

type appMsg struct {
	MsgType appMsgType `serialize:"true"`
	Bytes   []byte     `serialize:"true"`
	txID    ids.ID
	tx      *Tx
	ethData []EthData
	txs     []*types.Transaction
}

func encodeAtmData(c codec.Manager, txID ids.ID) ([]byte, error) {
	am := &appMsg{
		MsgType: atmDataType,
		Bytes:   txID[:],
	}

	return c.Marshal(codecVersion, am)
}

func encodeAtmTx(c codec.Manager, tx *Tx) ([]byte, error) {
	bytes, err := c.Marshal(codecVersion, tx)
	if err != nil {
		return nil, err
	}

	am := &appMsg{
		MsgType: atmTxType,
		Bytes:   bytes,
	}

	return c.Marshal(codecVersion, am)
}

func encodeEthData(c codec.Manager, ethData []EthData) ([]byte, error) {
	if len(ethData) > maxGossipEthDataSize {
		return nil, fmt.Errorf("could not encodeEthData, too many tx hashes")
	}

	bytes, err := rlp.EncodeToBytes(ethData)
	if err != nil {
		return nil, err
	}

	am := &appMsg{
		MsgType: ethDataType,
		Bytes:   bytes,
	}

	return c.Marshal(codecVersion, am)
}

func encodeEthTxs(c codec.Manager, ethTxs []*types.Transaction) ([]byte, error) {
	if len(ethTxs) > maxGossipEthTxsSize {
		return nil, fmt.Errorf("could not encodeEthTxs, too many txs")
	}

	bytes, err := rlp.EncodeToBytes(ethTxs)
	if err != nil {
		return nil, err
	}

	am := &appMsg{
		MsgType: ethTxsType,
		Bytes:   bytes,
	}

	return c.Marshal(codecVersion, am)
}

func decodeToAppMsg(c codec.Manager, bytes []byte) (*appMsg, error) {
	am := &appMsg{}
	if _, err := c.Unmarshal(bytes, am); err != nil {
		log.Debug(fmt.Sprintf("could not decode AppRequest msg, error %v", err))
		return nil, fmt.Errorf("could not decode AppRequest msg")
	}

	switch am.MsgType {
	case atmDataType:
		txID, err := ids.ToID(am.Bytes)
		if err != nil {
			return nil, fmt.Errorf("TxID bytes cannot be decoded into txID, error %s", err)
		}
		am.txID = txID
		return am, nil

	case atmTxType:
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

	case ethDataType:
		dataList := make([]EthData, 0)
		if err := rlp.DecodeBytes(am.Bytes, &dataList); err != nil {
			return nil, fmt.Errorf("could not decode AppRequest msg carrying eth hashes, error %v", err)
		}
		if len(dataList) > maxGossipEthDataSize {
			return nil, fmt.Errorf("Invalid AppRequest msg, too many tx hashes")
		}
		am.ethData = dataList
		return am, nil

	case ethTxsType:
		ethTxs := make([]*types.Transaction, 0)
		if err := rlp.DecodeBytes(am.Bytes, &ethTxs); err != nil {
			return nil, fmt.Errorf("could not decode AppRequest msg carrying eth txs, error %v", err)
		}
		if len(ethTxs) > maxGossipEthTxsSize {
			return nil, fmt.Errorf("Invalid AppRequest msg, too many txs")
		}
		am.txs = ethTxs
		return am, nil

	default:
		return nil, fmt.Errorf("unknown AppRequest msg type: %d", am.MsgType)
	}
}
