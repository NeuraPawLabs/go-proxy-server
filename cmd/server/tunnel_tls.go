package main

import (
	"crypto/tls"

	"github.com/apeming/go-proxy-server/internal/tunnel"
)

func loadTunnelServerTLSConfig(certFile, keyFile string, allowInsecure bool) (*tls.Config, error) {
	return tunnel.LoadServerTLSConfig(certFile, keyFile, allowInsecure)
}

func loadTunnelClientTLSConfig(serverAddr, caFile, serverName string, insecureSkipVerify, allowInsecure bool) (*tls.Config, error) {
	return tunnel.LoadClientTLSConfig(serverAddr, caFile, serverName, insecureSkipVerify, allowInsecure)
}
