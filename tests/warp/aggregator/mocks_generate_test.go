package aggregator

//go:generate go tool -modfile=../../../tools/go.mod mockgen -package=$GOPACKAGE -destination=mock_signature_getter.go . SignatureGetter
