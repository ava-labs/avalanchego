package main

type MigrationManager struct {
}

func NewMigrationManager() *MigrationManager {
	return &MigrationManager{}
}

func (m MigrationManager) ShouldMigrate() bool {
	//todo implement db check logic here
	return true
}

func (m MigrationManager) Migrate(binaryManager *BinaryManager) {
	//todo specify what are the paths and versions here
	binaryManager.prevVsApp.path = "/Users/pedro/go/src/github.com/ava-labs/avalanchego/build/avalanchego"
	binaryManager.prevVsApp.setup = true

	binaryManager.currVsApp.path = "/Users/pedro/go/src/github.com/ava-labs/avalanchego-internal/build/avalanchego-1.3.2"
	binaryManager.currVsApp.setup = true

}
