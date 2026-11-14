package transfer

import "io/fs"

// Windows needs an open handle for a file ID; size and modification time are
// checked instead.
func fileIdentity(fs.FileInfo) string { return "" }
