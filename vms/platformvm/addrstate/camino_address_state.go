package addrstate

type (
	AddressState    uint64
	AddressStateBit uint8
)

// AddressState flags, max 63
const (
	// Bits

	AddressStateBitRoleAdmin AddressStateBit = 0 // super role

	AddressStateBitRoleKYC                     AddressStateBit = 1 // allows to set KYCVerified and KYCExpired
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

	AddressStateRoleAdmin                   AddressState = AddressState(1) << AddressStateBitRoleAdmin                   // 0b1
	AddressStateRoleKYC                     AddressState = AddressState(1) << AddressStateBitRoleKYC                     // 0b10
	AddressStateRoleOffersAdmin             AddressState = AddressState(1) << AddressStateBitRoleOffersAdmin             // 0b100
	AddressStateRoleConsortiumAdminProposer AddressState = AddressState(1) << AddressStateBitRoleConsortiumAdminProposer // 0b1000
	AddressStateRoleAll                     AddressState = AddressStateRoleAdmin | AddressStateRoleKYC |                 // 0b1111
		AddressStateRoleOffersAdmin | AddressStateRoleConsortiumAdminProposer

	AddressStateKYCVerified AddressState = AddressState(1) << AddressStateBitKYCVerified    // 0b0100000000000000000000000000000000
	AddressStateKYCExpired  AddressState = AddressState(1) << AddressStateBitKYCExpired     // 0b1000000000000000000000000000000000
	AddressStateKYCAll      AddressState = AddressStateKYCVerified | AddressStateKYCExpired // 0b1100000000000000000000000000000000

	AddressStateConsortiumMember AddressState = AddressState(1) << AddressStateBitConsortium            // 0b0100000000000000000000000000000000000000
	AddressStateNodeDeferred     AddressState = AddressState(1) << AddressStateBitNodeDeferred          // 0b1000000000000000000000000000000000000000
	AddressStateVotableBits      AddressState = AddressStateConsortiumMember | AddressStateNodeDeferred // 0b1100000000000000000000000000000000000000

	AddressStateOffersCreator  AddressState = AddressState(1) << AddressStateBitOffersCreator  // 0b00100000000000000000000000000000000000000000000000000
	AddressStateCaminoProposer AddressState = AddressState(1) << AddressStateBitCaminoProposer // 0b01000000000000000000000000000000000000000000000000000

	AddressStateAthensPhaseBits = AddressStateRoleOffersAdmin | AddressStateOffersCreator
	AddressStateBerlinPhaseBits = AddressStateCaminoProposer | AddressStateRoleOffersAdmin

	AddressStateValidBits = AddressStateRoleAll | AddressStateKYCAll | AddressStateVotableBits |
		AddressStateAthensPhaseBits |
		AddressStateBerlinPhaseBits // 0b1100000000001100001100000000000000000000000000001111
)

func (as AddressState) Is(state AddressState) bool {
	return as&state == state
}

func (as AddressState) IsNot(state AddressState) bool {
	return as&state != state
}
