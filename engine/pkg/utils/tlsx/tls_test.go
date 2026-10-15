package tlsx

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// genSelfSignedCA 生成自签名 CA 证书和私钥，写入指定路径
func genSelfSignedCA(t *testing.T, dir string) (caFile, caKeyFile string, caCert *x509.Certificate, caKey *ecdsa.PrivateKey) {
	t.Helper()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}

	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	require.NoError(t, err)

	caCert, err = x509.ParseCertificate(caDER)
	require.NoError(t, err)

	caFile = filepath.Join(dir, "ca.pem")
	caKeyFile = filepath.Join(dir, "ca-key.pem")

	writePEM(t, caFile, "CERTIFICATE", caDER)
	keyDER, err := x509.MarshalECPrivateKey(caKey)
	require.NoError(t, err)
	writePEM(t, caKeyFile, "EC PRIVATE KEY", keyDER)

	return caFile, caKeyFile, caCert, caKey
}

// genSignedCert 用 CA 签发一张证书
func genSignedCert(t *testing.T, dir, name string, caCert *x509.Certificate, caKey *ecdsa.PrivateKey) (certFile, keyFile string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		DNSNames:     []string{"localhost", name},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	require.NoError(t, err)

	certFile = filepath.Join(dir, name+".pem")
	keyFile = filepath.Join(dir, name+"-key.pem")

	writePEM(t, certFile, "CERTIFICATE", certDER)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	writePEM(t, keyFile, "EC PRIVATE KEY", keyDER)

	return certFile, keyFile
}

func writePEM(t *testing.T, path, blockType string, der []byte) {
	t.Helper()
	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()
	require.NoError(t, pem.Encode(f, &pem.Block{Type: blockType, Bytes: der}))
}

// ─── LoadServerTLS ──────────────────────────────────────────

func TestLoadServerTLS_WithCA(t *testing.T) {
	dir := t.TempDir()
	caFile, _, caCert, caKey := genSelfSignedCA(t, dir)
	serverCert, serverKey := genSignedCert(t, dir, "server", caCert, caKey)

	cfg, err := LoadServerTLS(serverCert, serverKey, caFile)
	require.NoError(t, err)
	assert.Len(t, cfg.Certificates, 1)
	assert.Equal(t, tls.RequireAndVerifyClientCert, cfg.ClientAuth)
	assert.NotNil(t, cfg.ClientCAs)
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
}

func TestLoadServerTLS_WithoutCA(t *testing.T) {
	dir := t.TempDir()
	_, _, caCert, caKey := genSelfSignedCA(t, dir)
	serverCert, serverKey := genSignedCert(t, dir, "server", caCert, caKey)

	cfg, err := LoadServerTLS(serverCert, serverKey, "")
	require.NoError(t, err)
	assert.Len(t, cfg.Certificates, 1)
	assert.Equal(t, tls.NoClientCert, cfg.ClientAuth) // 默认值
	assert.Nil(t, cfg.ClientCAs)
}

func TestLoadServerTLS_BadCertPath(t *testing.T) {
	_, err := LoadServerTLS("/nonexistent/cert.pem", "/nonexistent/key.pem", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "load key pair")
}

func TestLoadServerTLS_BadCAPath(t *testing.T) {
	dir := t.TempDir()
	_, _, caCert, caKey := genSelfSignedCA(t, dir)
	serverCert, serverKey := genSignedCert(t, dir, "server", caCert, caKey)

	_, err := LoadServerTLS(serverCert, serverKey, "/nonexistent/ca.pem")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "read CA file")
}

func TestLoadServerTLS_InvalidCA(t *testing.T) {
	dir := t.TempDir()
	_, _, caCert, caKey := genSelfSignedCA(t, dir)
	serverCert, serverKey := genSignedCert(t, dir, "server", caCert, caKey)

	badCA := filepath.Join(dir, "bad-ca.pem")
	require.NoError(t, os.WriteFile(badCA, []byte("not a pem"), 0600))

	_, err := LoadServerTLS(serverCert, serverKey, badCA)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse CA")
}

// ─── LoadClientTLS ──────────────────────────────────────────

func TestLoadClientTLS_Full(t *testing.T) {
	dir := t.TempDir()
	caFile, _, caCert, caKey := genSelfSignedCA(t, dir)
	clientCert, clientKey := genSignedCert(t, dir, "client", caCert, caKey)

	cfg, err := LoadClientTLS(clientCert, clientKey, caFile, "localhost", false)
	require.NoError(t, err)
	assert.Len(t, cfg.Certificates, 1)
	assert.NotNil(t, cfg.RootCAs)
	assert.Equal(t, "localhost", cfg.ServerName)
	assert.False(t, cfg.InsecureSkipVerify)
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
}

func TestLoadClientTLS_NoCert(t *testing.T) {
	dir := t.TempDir()
	caFile, _, _, _ := genSelfSignedCA(t, dir)

	cfg, err := LoadClientTLS("", "", caFile, "localhost", false)
	require.NoError(t, err)
	assert.Empty(t, cfg.Certificates)
	assert.NotNil(t, cfg.RootCAs)
}

func TestLoadClientTLS_NoCA(t *testing.T) {
	cfg, err := LoadClientTLS("", "", "", "localhost", true)
	require.NoError(t, err)
	assert.Nil(t, cfg.RootCAs)
	assert.True(t, cfg.InsecureSkipVerify)
}

func TestLoadClientTLS_BadCertPath(t *testing.T) {
	_, err := LoadClientTLS("/nonexistent/cert.pem", "/nonexistent/key.pem", "", "", false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "load key pair")
}

func TestLoadClientTLS_BadCAPath(t *testing.T) {
	_, err := LoadClientTLS("", "", "/nonexistent/ca.pem", "", false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "read CA file")
}

func TestLoadClientTLS_InvalidCA(t *testing.T) {
	dir := t.TempDir()
	badCA := filepath.Join(dir, "bad-ca.pem")
	require.NoError(t, os.WriteFile(badCA, []byte("not a pem"), 0600))

	_, err := LoadClientTLS("", "", badCA, "", false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse CA")
}

// ─── 集成：mTLS 握手 ──────────────────────────────────────────

func TestMTLSHandshake(t *testing.T) {
	dir := t.TempDir()
	caFile, _, caCert, caKey := genSelfSignedCA(t, dir)
	serverCertFile, serverKeyFile := genSignedCert(t, dir, "server", caCert, caKey)
	clientCertFile, clientKeyFile := genSignedCert(t, dir, "client", caCert, caKey)

	serverTLS, err := LoadServerTLS(serverCertFile, serverKeyFile, caFile)
	require.NoError(t, err)

	clientTLS, err := LoadClientTLS(clientCertFile, clientKeyFile, caFile, "localhost", false)
	require.NoError(t, err)

	// 启动 TLS 服务端
	ln, err := tls.Listen("tcp", "127.0.0.1:0", serverTLS)
	require.NoError(t, err)
	defer ln.Close()

	done := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		tlsConn := conn.(*tls.Conn)
		done <- tlsConn.Handshake()
	}()

	// 客户端连接
	clientConn, err := tls.Dial("tcp", ln.Addr().String(), clientTLS)
	require.NoError(t, err)
	defer clientConn.Close()

	require.NoError(t, clientConn.Handshake())

	// 服务端也应该握手成功
	require.NoError(t, <-done)
}
