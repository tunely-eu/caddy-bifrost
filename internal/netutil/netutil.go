package netutil

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/caddyserver/caddy/v2"
)

var errClientHelloParsed = errors.New("client hello parsed")

func PeekClientHelloServerName(conn net.Conn) (string, net.Conn, error) {
	var replay bytes.Buffer
	peekConn := &readWriteConn{
		Conn: conn,
		r:    io.TeeReader(conn, &replay),
		w:    io.Discard,
	}

	var serverName string
	err := tls.Server(peekConn, &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			serverName = hello.ServerName
			return nil, errClientHelloParsed
		},
	}).Handshake()
	if err != nil && !errors.Is(err, errClientHelloParsed) {
		return "", nil, err
	}

	return serverName, &prefixConn{
		Conn: conn,
		r:    io.MultiReader(bytes.NewReader(replay.Bytes()), conn),
	}, nil
}

type readWriteConn struct {
	net.Conn
	r io.Reader
	w io.Writer
}

func (c *readWriteConn) Read(p []byte) (int, error) {
	return c.r.Read(p)
}

func (c *readWriteConn) Write(p []byte) (int, error) {
	return c.w.Write(p)
}

type prefixConn struct {
	net.Conn
	r io.Reader
}

func (c *prefixConn) Read(p []byte) (int, error) {
	return c.r.Read(p)
}

func DialAddress(ctx context.Context, address string) (net.Conn, error) {
	var dialer net.Dialer
	network, target := SplitAddress(address)
	return dialer.DialContext(ctx, network, target)
}

func ListenCaddy(ctx context.Context, address string) (net.Listener, error) {
	networkAddress, err := caddy.ParseNetworkAddress(address)
	if err != nil {
		return nil, err
	}
	if networkAddress.PortRangeSize() != 1 {
		return nil, fmt.Errorf("listener address %q must resolve to exactly one listener", address)
	}
	listener, err := networkAddress.Listen(ctx, 0, net.ListenConfig{})
	if err != nil {
		return nil, err
	}
	streamListener, ok := listener.(net.Listener)
	if !ok {
		if closer, ok := listener.(io.Closer); ok {
			_ = closer.Close()
		}
		return nil, fmt.Errorf("listener address %q did not create a stream listener", address)
	}
	return streamListener, nil
}

func SplitAddress(address string) (string, string) {
	switch {
	case strings.HasPrefix(address, "unix//"):
		return "unix", strings.TrimPrefix(address, "unix//")
	case strings.HasPrefix(address, "unix:"):
		return "unix", strings.TrimPrefix(address, "unix:")
	case strings.HasPrefix(address, "tcp//"):
		return "tcp", strings.TrimPrefix(address, "tcp//")
	case strings.HasPrefix(address, "tcp:"):
		return "tcp", strings.TrimPrefix(address, "tcp:")
	default:
		return "tcp", address
	}
}

func CloseOnContext(ctx context.Context, conns ...net.Conn) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			for _, conn := range conns {
				_ = conn.Close()
			}
		case <-done:
		}
	}()
	return func() {
		close(done)
	}
}
