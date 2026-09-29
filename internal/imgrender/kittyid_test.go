package imgrender

import "testing"

func TestNextKittyID(t *testing.T) {
	tests := []struct {
		prev, want uint32
	}{
		{0, 1},
		{1, 2},
		{41, 42},
		{MaxDynamicKittyID - 1, MaxDynamicKittyID},
		{MaxDynamicKittyID, 1}, // wraps
		{KittyReservedMin, 1},  // never enters the reserved range
		{ViewerKittyID, 1},     // a fixed id is not a valid previous id
		{0xFFFFFFFF, 1},        // nor is anything above 24 bits
		{0x01000000, 1},        // ids stay three-rune placeholders
		{MaxDynamicKittyID - 2, MaxDynamicKittyID - 1},
	}
	for _, tt := range tests {
		if got := NextKittyID(tt.prev); got != tt.want {
			t.Errorf("NextKittyID(%#x) = %#x, want %#x", tt.prev, got, tt.want)
		}
	}
}

func TestKittyIDRanges(t *testing.T) {
	if MaxDynamicKittyID+1 != KittyReservedMin {
		t.Errorf("dynamic range must end right below the reserved range")
	}
	if ViewerKittyID < KittyReservedMin || ViewerKittyID > 0xFFFFFF {
		t.Errorf("ViewerKittyID %#x outside the reserved range", ViewerKittyID)
	}
}
