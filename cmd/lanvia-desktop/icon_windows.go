//go:build windows

package main

import _ "embed"

// Wails uses this PNG when its default icon resource (ID 3) is absent.
// go-winres simply writes resource ID 1, so relying on EXE resources alone
// leaves title bars with the default Windows icon.
//
//go:embed build/icon-small-32.png
var applicationIcon []byte
