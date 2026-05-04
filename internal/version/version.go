// ABOUTME: Version information for the player
// ABOUTME: Used in device_info sent during handshake; patched at link time via -X
package version

// Version is the player version. Declared as var (not const) so the release
// workflow can inject the tag string with -ldflags "-X .../version.Version=v1.6.3".
// Linker -X only patches package-level string vars; constants are inlined and
// silently ignored. Pre-1.6.3 this was a const, which is why every release
// binary reported the hardcoded default regardless of the tag it shipped under.
var (
	Version      = "1.6.3"
	Product      = "Sendspin Go Player"
	Manufacturer = "sendspin-go"
)
