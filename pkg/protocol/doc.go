// ABOUTME: Sendspin wire protocol package
// ABOUTME: Defines protocol messages and WebSocket client
// Package protocol implements the Sendspin wire protocol.
//
// It provides the message types plus a Client (player side) and ServerConn
// helpers for communicating with Sendspin servers over WebSocket.
//
// Example:
//
//	client := protocol.NewClient(protocol.Config{
//		ServerAddr: "localhost:8927",
//		ClientID:   "my-client",
//		Name:       "My Player",
//	})
//	if err := client.Connect(); err != nil {
//		// handle handshake failure
//	}
package protocol
