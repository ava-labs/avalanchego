package wasmvm

import "github.com/ava-labs/gecko/database"

type tx interface {
	SyntacticVerify() error
	SemanticVerify(database.Database) error
	Accept()

	// To be called when tx is created or unmarshalled
	initialize(*VM) error
}
