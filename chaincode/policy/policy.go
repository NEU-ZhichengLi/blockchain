package policy

import "fmt"

const (
	OwnerMSP        = "Org1MSP"
	OwnerCommonName = "User1"
)

func ValidatePublisher(mspID, commonName string) error {
	if mspID != OwnerMSP {
		return fmt.Errorf("publisher MSP %q is not authorized", mspID)
	}
	if commonName != OwnerCommonName {
		return fmt.Errorf("publisher certificate %q is not authorized", commonName)
	}
	return nil
}

func ValidateNextWindow(hasLatest bool, current, next uint64) error {
	if !hasLatest {
		if next != 1 {
			return fmt.Errorf("first window must be 1, got %d", next)
		}
		return nil
	}
	if current == ^uint64(0) || next != current+1 {
		return fmt.Errorf("window %d does not immediately follow current window %d", next, current)
	}
	return nil
}
