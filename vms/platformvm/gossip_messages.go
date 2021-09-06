package platformvm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
)

type appMsgType byte

const (
	// Enum specifying the content of app gossiped messages
	txIDType appMsgType = iota + 1
	txType
)

type appMsg struct {
	ContentType appMsgType `serialize:"true"`
	Bytes       []byte     `serialize:"true"`

	// Must only be accessed if [ContentType] == txIDType
	txID ids.ID

	// Must only be accessed if [ContentType] == txType
	tx *Tx
}

func encodeTxID(c codec.Manager, txID ids.ID) ([]byte, error) {
	am := &appMsg{
		ContentType: txIDType,
		Bytes:       txID[:],
	}

	return c.Marshal(codecVersion, am)
}

func encodeTx(c codec.Manager, tx *Tx) ([]byte, error) {
	bytes, err := c.Marshal(codecVersion, tx)
	if err != nil {
		return nil, err
	}

	am := &appMsg{
		ContentType: txType,
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
	case txIDType:
		txID, err := ids.ToID(am.Bytes)
		if err != nil {
			return nil, fmt.Errorf("TxID bytes cannot be decoded into txID, error %v", err)
		}
		am.txID = txID
		return am, nil

	case txType:
		tx := &Tx{}
		_, err := c.Unmarshal(am.Bytes, tx)
		if err != nil {
			return nil, fmt.Errorf("could not decode tx, error %v", err)
		}
		unsignedBytes, err := c.Marshal(codecVersion, &tx.UnsignedTx)
		if err != nil {
			return nil, fmt.Errorf("could not decode unsignedTx, error %v", err)
		}
		tx.Initialize(unsignedBytes, am.Bytes)
		am.tx = tx
		return am, nil
	default:
		return nil, fmt.Errorf("unknown AppMsg type: %d", am.ContentType)
	}
}
