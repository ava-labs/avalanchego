package contract

//go:generate go tool mockgen -package=$GOPACKAGE -destination=mocks.go . BlockContext,AccessibleState,StateDB
