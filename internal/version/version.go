// ABOUTME: Version information for the player
// ABOUTME: Used in device_info sent during handshake; patched at link time via -X
package version

// Version is the player version. Declared as var (not const) so the release
// workflow can inject the tag string with -ldflags "-X .../version.Version=...".
// Go's linker -X flag only patches package-level string vars; constants are
// inlined at every callsite and silently ignored.
var (
	Version      = "1.6.3"
	Product      = "Sendspin Go Player"
	Manufacturer = "sendspin-go"
)
