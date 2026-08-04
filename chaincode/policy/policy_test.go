package policy

import "testing"

func TestValidatePublisher(t *testing.T) {
	if err := ValidatePublisher("Org1MSP", "User1"); err != nil {
		t.Fatalf("valid owner rejected: %v", err)
	}
	for _, claims := range [][2]string{
		{"Org2MSP", "User1"},
		{"Org1MSP", "CloudServer"},
		{"Org1MSP", ""},
	} {
		if err := ValidatePublisher(claims[0], claims[1]); err == nil {
			t.Fatalf("unauthorized publisher accepted: %q/%q", claims[0], claims[1])
		}
	}
}

func TestValidateNextWindow(t *testing.T) {
	if err := ValidateNextWindow(false, 0, 1); err != nil {
		t.Fatalf("first window rejected: %v", err)
	}
	if err := ValidateNextWindow(false, 0, 2); err == nil {
		t.Fatal("non-one first window accepted")
	}
	if err := ValidateNextWindow(true, 7, 8); err != nil {
		t.Fatalf("contiguous window rejected: %v", err)
	}
	for _, next := range []uint64{6, 7, 9} {
		if err := ValidateNextWindow(true, 7, next); err == nil {
			t.Fatalf("non-contiguous window %d accepted", next)
		}
	}
}
