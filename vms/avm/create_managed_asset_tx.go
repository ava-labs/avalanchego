package avm

// CreateManagedAssetTx creates an asset such that each UTXO with this
// asset ID may be consumed by the manager of the asset
type CreateManagedAssetTx struct {
	CreateAssetTx `serialize:"true"`
}
