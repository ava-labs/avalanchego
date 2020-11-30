package secp256k1fx

// ChangeAssetManagerOperation changes the manager of a managed asset
type ChangeAssetManagerOperation struct {
	Input              `serialize:"true"`
	AssetManagerOutput `serialize:"true"`
}
