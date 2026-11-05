package tray

import "strings"

// escapeMnemonics escapes underscores in s so that dbusmenu doesn't treat them as mnemonic markers.
func escapeMnemonics(s string) string {
	return strings.ReplaceAll(s, "_", "__")
}
