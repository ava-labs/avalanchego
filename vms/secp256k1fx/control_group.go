package secp256k1fx

/*
const maxAddressesSize = 256

var errAddressesNotUnique = errors.New("addresses aren't unique")

// ControlGroup is a group of addresses that, together, control something.
// [threshold] signatures from [addresses] are required to prove
// that the control group assents to something
type ControlGroup struct {
	Threshold uint16        `serialize:"true"`
	Addresses []ids.ShortID `serialize:"true"`
}

// Verify returns nil iff [owner] is well-formed
func (cg *ControlGroup) Verify() error {
	switch {
	case len(cg.Addresses) > maxAddressesSize:
		return fmt.Errorf("control group has %d addresses but maximum amount it %d",
			len(cg.Addresses), cg.Threshold)
	case len(cg.Addresses) < int(cg.Threshold):
		return fmt.Errorf("control group has %d addresses but threshold %d. Should have more addresses",
			len(cg.Addresses), cg.Threshold)
	case !ids.IsUniqueShortIDs(cg.Addresses):
		return errAddressesNotUnique
	}
	return nil
}

// AddressesSet returns cg.Addresses as a set
func (cg *ControlGroup) AddressesSet() ids.ShortSet {
	set := ids.ShortSet{}
	set.Add(cg.Addresses...)
	return set
}
*/
