package testutil

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func StartTLSOrigin(t *testing.T, cert tls.Certificate) (string, func()) {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	})
	if err != nil {
		t.Fatalf("listen origin: %v", err)
	}
	return serveOrigin(t, ln)
}

func StartHTTPOrigin(t *testing.T) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen origin: %v", err)
	}
	return serveOrigin(t, ln)
}

func serveOrigin(t *testing.T, ln net.Listener) (string, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					return
				}
			}
			go func(conn net.Conn) {
				defer conn.Close()
				reader := bufio.NewReader(conn)
				for {
					line, err := reader.ReadString('\n')
					if err != nil || line == "\r\n" {
						break
					}
				}
				_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 10\r\n\r\nbifrost-ok"))
			}(conn)
		}
	}()
	return ln.Addr().String(), func() {
		cancel()
		_ = ln.Close()
	}
}

func WaitHTTPSResponse(t *testing.T, addr string, serverName string, caPEM []byte) []byte {
	t.Helper()
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("append origin ca")
	}
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 300 * time.Millisecond}, "tcp", addr, &tls.Config{
			MinVersion: tls.VersionTLS12,
			RootCAs:    roots,
			ServerName: serverName,
		})
		if err != nil {
			lastErr = err
			time.Sleep(50 * time.Millisecond)
			continue
		}
		_ = conn.SetDeadline(time.Now().Add(time.Second))
		if _, err := conn.Write([]byte("GET / HTTP/1.1\r\nHost: " + serverName + "\r\nConnection: close\r\n\r\n")); err != nil {
			lastErr = err
			_ = conn.Close()
			time.Sleep(50 * time.Millisecond)
			continue
		}
		response, err := io.ReadAll(conn)
		_ = conn.Close()
		if err == nil && len(response) > 0 {
			return response
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("wait HTTPS response: %v", lastErr)
	return nil
}

func WaitHTTPTransportResponse(t *testing.T, transport http.RoundTripper, url string) []byte {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		resp, err := transport.RoundTrip(req)
		if err != nil {
			lastErr = err
			time.Sleep(50 * time.Millisecond)
			continue
		}
		response, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err == nil {
			return []byte(fmt.Sprintf("%s %s\r\n%s", resp.Proto, resp.Status, response))
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("wait HTTP transport response: %v", lastErr)
	return nil
}

func FreeTCPAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen free addr: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func WriteTestCertFiles(t *testing.T, dir string, name string) (string, string, string) {
	t.Helper()
	certPEM, keyPEM := MakeTestCert(t, name)
	certFile := filepath.Join(dir, name+".crt")
	keyFile := filepath.Join(dir, name+".key")
	caFile := filepath.Join(dir, name+"-ca.crt")
	if err := os.WriteFile(certFile, certPEM, 0o644); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	if err := os.WriteFile(caFile, certPEM, 0o644); err != nil {
		t.Fatalf("write ca: %v", err)
	}
	return certFile, keyFile, caFile
}

func MakeTestCert(t *testing.T, name string) ([]byte, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: name,
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{name},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return certPEM, keyPEM
}
