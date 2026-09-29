//go:build !windows

package imageviewer

import "io"

// enableVT is a no-op outside Windows: terminals interpret escape
// sequences already.
func enableVT(io.Writer) (restore func()) { return func() {} }
