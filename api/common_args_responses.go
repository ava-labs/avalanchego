package api

import "github.com/ava-labs/avalanchego/ids"

// This file contains structs used in arguments and responses in services

// SuccessResponse indicates success of an API call
type SuccessResponse struct {
	Success bool `json:"success"`
}

// JsonTxID contains the ID of a transaction
type JsonTxID struct {
	TxID ids.ID `json:"txID"`
}

// UserPass contains a username and a password
type UserPass struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// JsonAddress contains an address
type JsonAddress struct {
	Address string `json:"address"`
}

// JsonAddresses contains a list of address
type JsonAddresses struct {
	Addresses []string `json:"addresses"`
}

// ChangeAddr is the address change is sent to, if any
type JsonChangeAddr struct {
	ChangeAddr string `json:"changeAddr"`
}

// JsonTxIDChangeAddr is a tx ID and change address
type JsonTxIDChangeAddr struct {
	JsonTxID
	JsonChangeAddr
}
