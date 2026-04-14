package tunnel

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
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestTunnelServerAndClientRelayTraffic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverTLS, clientTLS := newTestTLSConfigs(t)
	echoListener := startEchoServer(t)
	defer echoListener.Close()

	server := NewServer("127.0.0.1:0", "127.0.0.1", "secret-token")
	server.PendingTimeout = 3 * time.Second
	server.TLSConfig = serverTLS
	if err := server.Start(ctx); err != nil {
		t.Fatalf("start tunnel server: %v", err)
	}

	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- server.Wait()
	}()

	publicPortCh := make(chan int, 1)
	client := NewClient(server.ControlAddr, "secret-token", "echo", echoListener.Addr().String(), 0)
	client.ReconnectDelay = 100 * time.Millisecond
	client.TLSConfig = clientTLS
	client.OnConnected = func(publicPort int) {
		select {
		case publicPortCh <- publicPort:
		default:
		}
	}

	clientErrCh := make(chan error, 1)
	go func() {
		clientErrCh <- client.Run(ctx)
	}()

	var publicPort int
	select {
	case publicPort = <-publicPortCh:
	case err := <-clientErrCh:
		t.Fatalf("tunnel client exited before ready: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for tunnel to become ready")
	}

	publicConn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", publicPort), 5*time.Second)
	if err != nil {
		t.Fatalf("dial tunnel public port: %v", err)
	}
	defer publicConn.Close()

	if _, err := io.WriteString(publicConn, "ping\n"); err != nil {
		t.Fatalf("write to tunnel: %v", err)
	}

	line, err := bufio.NewReader(publicConn).ReadString('\n')
	if err != nil {
		t.Fatalf("read from tunnel: %v", err)
	}
	if line != "ping\n" {
		t.Fatalf("unexpected tunnel response: got %q want %q", line, "ping\\n")
	}

	cancel()

	select {
	case err := <-clientErrCh:
		if err != nil {
			t.Fatalf("client shutdown error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for client shutdown")
	}

	select {
	case err := <-serverErrCh:
		if err != nil {
			t.Fatalf("server shutdown error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server shutdown")
	}
}

func TestTunnelClientRejectsInvalidToken(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverTLS, clientTLS := newTestTLSConfigs(t)
	server := NewServer("127.0.0.1:0", "127.0.0.1", "secret-token")
	server.TLSConfig = serverTLS
	if err := server.Start(ctx); err != nil {
		t.Fatalf("start tunnel server: %v", err)
	}

	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- server.Wait()
	}()

	client := NewClient(server.ControlAddr, "wrong-token", "echo", "127.0.0.1:65535", 0)
	client.TLSConfig = clientTLS
	if err := client.Run(ctx); err == nil || !strings.Contains(err.Error(), "invalid tunnel token") {
		t.Fatalf("unexpected client error: %v", err)
	}

	cancel()
	select {
	case err := <-serverErrCh:
		if err != nil {
			t.Fatalf("server shutdown error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server shutdown")
	}
}

func TestReadJSONLineRejectsOversizedFrame(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader(strings.Repeat("a", maxMessageSize+1) + "\n"))
	var hs handshake
	if err := readJSONLine(reader, &hs); err != errMessageTooLarge {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOpenPendingConnectionRejectsWhenLimitReached(t *testing.T) {
	left1, right1 := net.Pipe()
	defer left1.Close()
	defer right1.Close()
	left2, right2 := net.Pipe()
	defer left2.Close()
	defer right2.Close()

	tunnel := &serverTunnel{
		server:  &Server{MaxPendingConnections: 1},
		pending: map[string]net.Conn{"existing": left1},
	}

	if err := tunnel.openPendingConnection(left2); err == nil || !strings.Contains(err.Error(), "too many pending") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegisterTunnelReplacesExistingName(t *testing.T) {
	server := NewServer("127.0.0.1:0", "127.0.0.1", "secret-token")
	server.AllowInsecure = true

	controlA, peerA := net.Pipe()
	defer peerA.Close()
	first, err := server.registerTunnel(controlA, handshake{Token: "secret-token", TunnelName: "demo", PublicPort: 0})
	if err != nil {
		t.Fatalf("register first tunnel: %v", err)
	}
	defer server.unregisterTunnel(first)

	controlB, peerB := net.Pipe()
	defer peerB.Close()
	second, err := server.registerTunnel(controlB, handshake{Token: "secret-token", TunnelName: "demo", PublicPort: 0})
	if err != nil {
		t.Fatalf("register replacement tunnel: %v", err)
	}
	defer server.unregisterTunnel(second)

	if got := server.getTunnel("demo"); got != second {
		t.Fatalf("expected active tunnel to be replacement, got %#v", got)
	}
}

func startEchoServer(t *testing.T) net.Listener {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("start echo server: %v", err)
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(conn)
		}
	}()

	return listener
}

func newTestTLSConfigs(t *testing.T) (*tls.Config, *tls.Config) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate private key: %v", err)
	}

	serialNumber, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		t.Fatalf("generate serial number: %v", err)
	}

	certTemplate := &x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IP(netip.MustParseAddr("127.0.0.1").AsSlice())},
	}

	der, err := x509.CreateCertificate(rand.Reader, certTemplate, certTemplate, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	serverCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	serverKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	serverCert, err := tls.X509KeyPair(serverCertPEM, serverKeyPEM)
	if err != nil {
		t.Fatalf("load key pair: %v", err)
	}

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(serverCertPEM) {
		t.Fatal("append root cert")
	}

	return &tls.Config{
			Certificates: []tls.Certificate{serverCert},
			MinVersion:   tls.VersionTLS12,
		}, &tls.Config{
			RootCAs:    roots,
			ServerName: "127.0.0.1",
			MinVersion: tls.VersionTLS12,
		}
}
