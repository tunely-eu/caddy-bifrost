package caddybifrost

import (
	"crypto/tls"
	"net"
	"testing"
	"time"
)

func TestPeekClientHelloServerNamePreservesBytes(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	type result struct {
		serverName string
		conn       net.Conn
		err        error
	}
	results := make(chan result, 1)
	go func() {
		serverName, replayConn, err := peekClientHelloServerName(serverConn)
		results <- result{serverName: serverName, conn: replayConn, err: err}
	}()

	client := tls.Client(clientConn, &tls.Config{
		MinVersion:         tls.VersionTLS12,
		ServerName:         "home.example.com",
		InsecureSkipVerify: true,
	})
	handshakeDone := make(chan error, 1)
	go func() {
		handshakeDone <- client.Handshake()
	}()

	select {
	case got := <-results:
		if got.err != nil {
			t.Fatalf("peekClientHelloServerName: %v", got.err)
		}
		if got.serverName != "home.example.com" {
			t.Fatalf("serverName = %q", got.serverName)
		}
		buf := make([]byte, 1)
		if _, err := got.conn.Read(buf); err != nil {
			t.Fatalf("read replayed client hello byte: %v", err)
		}
		if buf[0] != 0x16 {
			t.Fatalf("first replayed byte = %#x", buf[0])
		}
		_ = got.conn.Close()
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for client hello")
	}

	select {
	case <-handshakeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("client handshake did not unblock")
	}
}
