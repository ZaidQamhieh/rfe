//go:build !unix

package thumbs

import "os"

func fileIdentity(info os.FileInfo) string { return "" }
