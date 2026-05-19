// Package ws provides WebSocket constants used by cmd/kode/serve.go.
// Frame I/O is handled directly by golang.org/x/net/websocket.
package ws

const (
	OpText  = 1 // WebSocket text frame
	OpBinary = 2 // WebSocket binary frame
)
