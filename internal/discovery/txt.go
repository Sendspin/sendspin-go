// ABOUTME: Pure TXT record parsing for mDNS service entries
// ABOUTME: Converts []string of "key=value" entries to map[string]string

package discovery

import "strings"

// parseTXT converts an mDNS TXT record slice (each element "key=value")
// into a map. Empty strings are ignored. Keys without '=' are stored
// with an empty value. When a key appears multiple times, the last
// occurrence wins.
func parseTXT(fields []string) map[string]string {
	out := make(map[string]string, len(fields))
	for _, f := range fields {
		if f == "" {
			continue
		}
		if idx := strings.Index(f, "="); idx >= 0 {
			out[f[:idx]] = f[idx+1:]
		} else {
			out[f] = ""
		}
	}
	return out
}
