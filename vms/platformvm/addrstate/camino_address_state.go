package addrstate

type (
	AddressState    uint64
	AddressStateBit uint8
)

// AddressState flags, max 63
const (
	// Bits

	AddressStateBitRoleAdmin AddressStateBit = 0 // super role

	AddressStateBitRoleKYCAdmin                AddressStateBit = 1 // allows to set KYCVerified and KYCExpired
	AddressStateBitRoleOffersAdmin             AddressStateBit = 2 // allows to set OffersCreator
	AddressStateBitRoleConsortiumAdminProposer AddressStateBit = 3 // allows to create admin add/exclude member proposals

	AddressStateBitKYCVerified    AddressStateBit = 32
	AddressStateBitKYCExpired     AddressStateBit = 33
	AddressStateBitConsortium     AddressStateBit = 38
	AddressStateBitNodeDeferred   AddressStateBit = 39
	AddressStateBitOffersCreator  AddressStateBit = 50
	AddressStateBitCaminoProposer AddressStateBit = 51

	AddressStateBitMax AddressStateBit = 63

	// States

	AddressStateEmpty AddressState = 0

	AddressStateRoleAdmin                   = AddressState(1) << AddressStateBitRoleAdmin                   // 0b1
	AddressStateRoleKYCAdmin                = AddressState(1) << AddressStateBitRoleKYCAdmin                // 0b10
	AddressStateRoleOffersAdmin             = AddressState(1) << AddressStateBitRoleOffersAdmin             // 0b100
	AddressStateRoleConsortiumAdminProposer = AddressState(1) << AddressStateBitRoleConsortiumAdminProposer // 0b1000

	AddressStateKYCVerified = AddressState(1) << AddressStateBitKYCVerified // 0b0100000000000000000000000000000000
	AddressStateKYCExpired  = AddressState(1) << AddressStateBitKYCExpired  // 0b1000000000000000000000000000000000

	AddressStateConsortium   = AddressState(1) << AddressStateBitConsortium   // 0b0100000000000000000000000000000000000000
	AddressStateNodeDeferred = AddressState(1) << AddressStateBitNodeDeferred // 0b1000000000000000000000000000000000000000

	AddressStateOffersCreator  = AddressState(1) << AddressStateBitOffersCreator  // 0b0100000000000000000000000000000000000000000000000000
	AddressStateCaminoProposer = AddressState(1) << AddressStateBitCaminoProposer // 0b1000000000000000000000000000000000000000000000000000

	// Bit groups (as AddressState)

	AddressStateSunrisePhaseBits = AddressStateRoleAdmin | AddressStateRoleKYCAdmin | // 0b1100001100000000000000000000000000000011
		AddressStateKYCVerified | AddressStateKYCExpired | AddressStateConsortium |
		AddressStateNodeDeferred

	AddressStateAthensPhaseBits = AddressStateRoleOffersAdmin | AddressStateOffersCreator // 0b0100000000000000000000000000000000000000000000000100

	AddressStateBerlinPhaseBits = AddressStateCaminoProposer | AddressStateRoleConsortiumAdminProposer // 0b1000000000000000000000000000000000000000000000001000

	AddressStateValidBits = AddressStateSunrisePhaseBits | // 0b1100000000001100001100000000000000000000000000001111
		AddressStateAthensPhaseBits |
		AddressStateBerlinPhaseBits
)

func (as AddressState) Is(state AddressState) bool {
	return as&state == state
}

func (as AddressState) IsNot(state AddressState) bool {
	return as&state != state
}

func (asb AddressStateBit) ToAddressState() AddressState {
	return AddressState(1) << asb
}
