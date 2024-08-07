package addrstate

type (
	AddressState    uint64
	AddressStateBit uint8
)

// AddressState flags, max 63
const (
	// Bits

	// Allow to set role bits
	AddressStateBitRoleAdmin AddressStateBit = 0
	// Allows to set KYC and KYB bits
	AddressStateBitRoleKYCAdmin AddressStateBit = 1
	// Allows to set OffersCreator bit
	AddressStateBitRoleOffersAdmin AddressStateBit = 2
	// Allows to create addMember and excludeMember admin-proposals
	AddressStateBitRoleConsortiumSecretary AddressStateBit = 3
	// Allows to set node deferred bit
	AddressStateBitRoleValidatorAdmin AddressStateBit = 4

	// Indicates that address passed KYC verification
	AddressStateBitKYCVerified AddressStateBit = 32
	// Indicates that address KYC verification is expired. (not yet implemented)
	AddressStateBitKYCExpired AddressStateBit = 33
	// Indicates that address is member of consortium
	AddressStateBitConsortium AddressStateBit = 38
	// Indicates that a node owned by this address (as consortium member) is deferred
	AddressStateBitNodeDeferred AddressStateBit = 39
	// Allows to create deposit offers
	AddressStateBitOffersCreator AddressStateBit = 50
	// Allows to create baseFee and feeDistribution proposals
	AddressStateBitFoundationAdmin AddressStateBit = 51

	AddressStateBitMax AddressStateBit = 63

	// States

	// 0b0000000000000000000000000000000000000000000000000000000000000000
	AddressStateEmpty AddressState = 0

	// 0b0000000000000000000000000000000000000000000000000000000000000001
	AddressStateRoleAdmin = AddressState(1) << AddressStateBitRoleAdmin
	// 0b0000000000000000000000000000000000000000000000000000000000000010
	AddressStateRoleKYCAdmin = AddressState(1) << AddressStateBitRoleKYCAdmin
	// 0b0000000000000000000000000000000000000000000000000000000000000100
	AddressStateRoleOffersAdmin = AddressState(1) << AddressStateBitRoleOffersAdmin
	// 0b0000000000000000000000000000000000000000000000000000000000001000
	AddressStateRoleConsortiumSecretary = AddressState(1) << AddressStateBitRoleConsortiumSecretary
	// 0b0000000000000000000000000000000000000000000000000000000000010000
	AddressStateRoleValidatorAdmin = AddressState(1) << AddressStateBitRoleValidatorAdmin

	// 0b0000000000000000000000000000000100000000000000000000000000000000
	AddressStateKYCVerified = AddressState(1) << AddressStateBitKYCVerified
	// 0b0000000000000000000000000000001000000000000000000000000000000000
	AddressStateKYCExpired = AddressState(1) << AddressStateBitKYCExpired
	// 0b0000000000000000000000000100000000000000000000000000000000000000
	AddressStateConsortium = AddressState(1) << AddressStateBitConsortium
	// 0b0000000000000000000000001000000000000000000000000000000000000000
	AddressStateNodeDeferred = AddressState(1) << AddressStateBitNodeDeferred
	// 0b0000000000000100000000000000000000000000000000000000000000000000
	AddressStateOffersCreator = AddressState(1) << AddressStateBitOffersCreator
	// 0b0000000000001000000000000000000000000000000000000000000000000000
	AddressStateFoundationAdmin = AddressState(1) << AddressStateBitFoundationAdmin

	// Bit groups (as AddressState)

	// 0b0000000000000000000000001100001100000000000000000000000000000011
	AddressStateSunrisePhaseBits = AddressStateRoleAdmin | AddressStateRoleKYCAdmin |
		AddressStateKYCVerified | AddressStateKYCExpired | AddressStateConsortium |
		AddressStateNodeDeferred
	// 0b0000000000000100000000000000000000000000000000000000000000000100
	AddressStateAthensPhaseBits = AddressStateRoleOffersAdmin | AddressStateOffersCreator
	// 0b0000000000001000000000000000000000000000000000000000000000001000
	AddressStateBerlinPhaseBits = AddressStateFoundationAdmin | AddressStateRoleConsortiumSecretary |
		AddressStateRoleValidatorAdmin
	// 0b0000000000001100000000001100001100000000000000000000000000011111
	AddressStateValidBits = AddressStateSunrisePhaseBits |
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
