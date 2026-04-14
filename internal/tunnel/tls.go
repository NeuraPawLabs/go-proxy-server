package tunnel

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
)

func LoadServerTLSConfig(certFile, keyFile string, allowInsecure bool) (*tls.Config, error) {
	if allowInsecure {
		if certFile != "" || keyFile != "" {
			return nil, fmt.Errorf("cannot combine -allow-insecure with -cert/-key")
		}
		return nil, nil
	}
	if certFile == "" || keyFile == "" {
		return nil, fmt.Errorf("tunnel server requires -cert and -key unless -allow-insecure is set")
	}

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load tunnel server certificate: %w", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}

func LoadClientTLSConfig(serverAddr, caFile, serverName string, insecureSkipVerify, allowInsecure bool) (*tls.Config, error) {
	if allowInsecure {
		if caFile != "" || serverName != "" || insecureSkipVerify {
			return nil, fmt.Errorf("cannot combine -allow-insecure with TLS verification flags")
		}
		return nil, nil
	}
	if caFile == "" && !insecureSkipVerify {
		return nil, fmt.Errorf("tunnel client requires -ca, -insecure-skip-verify, or -allow-insecure")
	}

	var roots *x509.CertPool
	if caFile != "" {
		caPEM, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read tunnel CA file: %w", err)
		}

		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("parse tunnel CA file: no certificates found")
		}
	} else {
		pool, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("load system cert pool: %w", err)
		}
		roots = pool
	}

	if serverName == "" {
		host, _, err := net.SplitHostPort(serverAddr)
		if err != nil {
			return nil, fmt.Errorf("parse tunnel server address: %w", err)
		}
		serverName = host
	}

	return &tls.Config{
		RootCAs:            roots,
		ServerName:         serverName,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: insecureSkipVerify,
	}, nil
}
