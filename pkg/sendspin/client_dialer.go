// ABOUTME: Outbound client discovery dialer for server-initiated mode
// ABOUTME: Converts discovery.ClientInfo into dialed WebSocket connections

package sendspin

import (
	"fmt"
	"strings"

	"github.com/Sendspin/sendspin-go/internal/discovery"
)

// clientInfoURL builds a ws:// URL from a discovered ClientInfo.
// Normalizes Path to ensure a single leading slash.
func clientInfoURL(info *discovery.ClientInfo) string {
	path := info.Path
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return fmt.Sprintf("ws://%s:%d%s", info.Host, info.Port, path)
}
