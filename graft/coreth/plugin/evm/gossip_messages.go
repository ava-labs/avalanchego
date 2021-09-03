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

type AppMsg struct {
	MsgType      appMsgType `serialize:"true"`
	Bytes        []byte     `serialize:"true"`
	appGossipObj interface{}
}

func encodeAtmData(c codec.Manager, txID ids.ID) ([]byte, error) {
	am := &AppMsg{
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

	am := &AppMsg{
		MsgType: atmTxType,
		Bytes:   bytes,
	}

	return c.Marshal(codecVersion, am)
}

type EthData struct {
	TxHash    common.Hash
	TxAddress common.Address
	TxNonce   uint64
}

func encodeEthData(c codec.Manager, ethData []EthData) ([]byte, error) {
	bytes, err := rlp.EncodeToBytes(ethData)
	if err != nil {
		return nil, err
	}

	am := &AppMsg{
		MsgType: ethDataType,
		Bytes:   bytes,
	}

	return c.Marshal(codecVersion, am)
}

func encodeEthTxs(c codec.Manager, ethTxs []*types.Transaction) ([]byte, error) {
	bytes, err := rlp.EncodeToBytes(ethTxs)
	if err != nil {
		return nil, err
	}

	am := &AppMsg{
		MsgType: ethTxsType,
		Bytes:   bytes,
	}

	return c.Marshal(codecVersion, am)
}

func decodeToAppMsg(c codec.Manager, bytes []byte) (*AppMsg, error) {
	appMsg := &AppMsg{}
	if _, err := c.Unmarshal(bytes, appMsg); err != nil {
		log.Debug(fmt.Sprintf("could not decode AppRequest msg, error %v", err))
		return nil, fmt.Errorf("could not decode AppRequest msg")
	}

	switch appMsg.MsgType {
	case atmDataType:
		txID, err := ids.ToID(appMsg.Bytes)
		if err != nil {
			log.Debug(fmt.Sprintf("TxID bytes cannot be decoded into txID, error %s", err))
			return nil, fmt.Errorf("bad atomicTxID AppMsg")
		}
		appMsg.appGossipObj = txID
		return appMsg, nil

	case atmTxType:
		tx := &Tx{}
		if _, err := c.Unmarshal(appMsg.Bytes, tx); err != nil {
			log.Debug(fmt.Sprintf("could not decode atomic tx, error %v", err))
			return nil, err
		}
		unsignedBytes, err := c.Marshal(codecVersion, &tx.UnsignedAtomicTx)
		if err != nil {
			log.Debug(fmt.Sprintf("could not decode unsigned atomic tx, error %v", err))
			return nil, err
		}
		tx.Initialize(unsignedBytes, appMsg.Bytes)
		appMsg.appGossipObj = tx
		return appMsg, nil

	case ethDataType:
		dataList := make([]EthData, 0)
		if err := rlp.DecodeBytes(appMsg.Bytes, &dataList); err != nil {
			log.Debug(fmt.Sprintf("could not decode AppRequest msg carrying eth hashes, error %v", err))
			return nil, fmt.Errorf("could not decode AppRequest msg with eth hashes")
		}
		appMsg.appGossipObj = dataList
		return appMsg, nil

	case ethTxsType:
		ethTxs := make([]*types.Transaction, 0)
		if err := rlp.DecodeBytes(appMsg.Bytes, &ethTxs); err != nil {
			log.Debug(fmt.Sprintf("could not decode AppRequest msg carrying eth txs, error %v", err))
			return nil, fmt.Errorf("could not decode AppRequest msg with eth txs")
		}
		appMsg.appGossipObj = ethTxs
		return appMsg, nil

	default:
		log.Debug(fmt.Sprintf("Unknown AppRequest msg txIDType %v", appMsg.MsgType))
		return nil, fmt.Errorf("unknown AppRequest msg txIDType")
	}
}
