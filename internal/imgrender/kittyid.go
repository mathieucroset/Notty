package imgrender

// Kitty image ids.
//
// Ids are kept within 24 bits so placeholder cells stay three runes (the id
// fits in the foreground color). The range is split in two:
//
//   - 1..MaxDynamicKittyID (0x000001–0xFFFEFF): dynamic ids, handed out with
//     [NextKittyID] by the preview for the images it transmits.
//   - KittyReservedMin..0xFFFFFF (0xFFFF00–0xFFFFFF): fixed ids for
//     well-known uses such as [ViewerKittyID]. Dynamic allocation never
//     returns them, so a fixed-id image can never replace a preview image
//     (or be deleted with one) on the same screen.
const (
	MaxDynamicKittyID uint32 = 0xFFFEFF
	KittyReservedMin  uint32 = 0xFFFF00
)

// ViewerKittyID is the fixed id of the image shown by the full-screen image
// viewer.
const ViewerKittyID uint32 = 0xFFFFFE

// NextKittyID returns the dynamic id that follows prev: prev+1, wrapping to
// 1 after [MaxDynamicKittyID]. A prev of 0 or outside the dynamic range
// (reserved or above 24 bits) also yields 1.
func NextKittyID(prev uint32) uint32 {
	if prev == 0 || prev >= MaxDynamicKittyID {
		return 1
	}
	return prev + 1
}
