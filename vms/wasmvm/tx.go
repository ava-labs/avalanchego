package wasmvm

import (
	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
)

type tx interface {
	ID() ids.ID
	SyntacticVerify() error
	SemanticVerify(database.Database) error

	// To be called when tx is created or unmarshalled
	initialize(*VM) error
}
