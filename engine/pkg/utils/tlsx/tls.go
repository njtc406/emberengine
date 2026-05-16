// Package tlsx 提供 TLS 配置加载工具函数，用于 gRPC 和 NATS 的 mTLS 支持。
package tlsx

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// LoadServerTLS 加载服务端 mTLS 配置。
// certFile/keyFile 为服务端证书和私钥路径；caFile 为 CA 证书路径（非空时启用客户端证书验证）。
func LoadServerTLS(certFile, keyFile, caFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("tlsx.LoadServerTLS: load key pair: %w", err)
	}

	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	if caFile != "" {
		caCert, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("tlsx.LoadServerTLS: read CA file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("tlsx.LoadServerTLS: failed to parse CA certificate")
		}
		cfg.ClientCAs = pool
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}

	return cfg, nil
}

// LoadClientTLS 加载客户端 mTLS 配置。
// certFile/keyFile 为客户端证书和私钥路径（可为空，仅做服务端验证）；
// caFile 为 CA 证书路径；serverName 用于 SNI 校验。
// insecureSkipVerify 仅用于开发/测试，生产环境应为 false。
func LoadClientTLS(certFile, keyFile, caFile, serverName string, insecureSkipVerify bool) (*tls.Config, error) {
	// 安全护栏：同时配置 CA 和 insecureSkipVerify 是矛盾的，拒绝这种配置
	if insecureSkipVerify && caFile != "" {
		return nil, fmt.Errorf("tlsx.LoadClientTLS: insecureSkipVerify=true conflicts with caFile=%q; "+
			"either trust the CA or skip verification, not both", caFile)
	}
	cfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		ServerName:         serverName,
		InsecureSkipVerify: insecureSkipVerify, // #nosec G402 -- 由调用方控制，仅开发环境使用
	}

	// 校验 cert/key 必须同时配置或同时为空
	if (certFile != "") != (keyFile != "") {
		return nil, fmt.Errorf("tlsx.LoadClientTLS: certFile and keyFile must both be set or both be empty (got certFile=%q, keyFile=%q)", certFile, keyFile)
	}

	// 加载客户端证书（用于 mTLS）
	if certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("tlsx.LoadClientTLS: load key pair: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}

	// 加载 CA 证书
	if caFile != "" {
		caCert, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("tlsx.LoadClientTLS: read CA file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("tlsx.LoadClientTLS: failed to parse CA certificate")
		}
		cfg.RootCAs = pool
	}

	return cfg, nil
}
