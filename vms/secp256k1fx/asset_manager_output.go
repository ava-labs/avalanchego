package secp256k1fx

// AssetManagerOutput is held by the manager of a managed asset
type AssetManagerOutput struct {
	OutputOwners `serialize:"true"`
}
