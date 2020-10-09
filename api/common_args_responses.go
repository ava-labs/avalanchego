package api

import "github.com/ava-labs/avalanchego/ids"

// This file contains structs used in arguments and responses in services

// SuccessResponse indicates success of an API call
type SuccessResponse struct {
	Success bool `json:"success"`
}

// JSONTxID contains the ID of a transaction
type JSONTxID struct {
	TxID ids.ID `json:"txID"`
}

// UserPass contains a username and a password
type UserPass struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// JSONAddress contains an address
type JSONAddress struct {
	Address string `json:"address"`
}

// JSONAddresses contains a list of address
type JSONAddresses struct {
	Addresses []string `json:"addresses"`
}

// ChangeAddr is the address change is sent to, if any
type JSONChangeAddr struct {
	ChangeAddr string `json:"changeAddr"`
}

// JSONTxIDChangeAddr is a tx ID and change address
type JSONTxIDChangeAddr struct {
	JSONTxID
	JSONChangeAddr
}

// JSONFromAddrs is a list of addresses to send funds from
type JSONFromAddrs struct {
	From []string `json:"from"`
}

// JSONSpendHeader is 3 arguments to a method that spends (including those with tx fees)
// 1) The username/password
// 2) The addresses used in the method
// 3) The address to send change to
type JSONSpendHeader struct {
	UserPass
	JSONFromAddrs
	JSONChangeAddr
}
