package api

import "github.com/ava-labs/gecko/ids"

// This file contains structs used in arguments and responses in services

// SuccessResponse indicates success of an API call
type SuccessResponse struct {
	Success bool `json:"success"`
}

// TxIDResponse contains the ID of a transaction
type TxIDResponse struct {
	TxID ids.ID `json:"txID"`
}

// UserPass contains a username and a password
type UserPass struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
