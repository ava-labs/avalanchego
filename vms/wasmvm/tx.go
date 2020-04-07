package wasmvm

import "github.com/ava-labs/gecko/database"

type tx interface {
	SyntacticVerify() error
	SemanticVerify(database.Database) error
	Accept()
	initialize(*VM)
}
