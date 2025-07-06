package chains

//go:generate go run go.uber.org/mock/mockgen -package=${GOPACKAGE}mock -destination=mocks/full_vm.go -mock_names=FullVM=FullVM . FullVM
