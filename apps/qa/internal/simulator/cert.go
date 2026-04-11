// Package simulator provides synthetic Personel agents for load testing.
//
// cert.go builds a three-tier test PKI that mirrors the layout described in
// docs/architecture/mtls-pki.md:
//
//	Test Root CA  (self-signed, in-memory)
//	  Tenant CA   (signed by root, per-scenario)
//	    Agent Cert (per-endpoint, 14-day validity, auto-issued on demand)
//
// The root and tenant CA private keys are generated once per TestCA instance
// and never leave memory. This makes the test PKI self-contained and
// deterministic when seeded.
package simulator

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

// TestCA is a self-contained test certificate authority that mirrors the
// Personel mTLS PKI layout. It is safe for concurrent use.
type TestCA struct {
	rootKey  *ecdsa.PrivateKey
	rootCert *x509.Certificate
	rootDER  []byte

	tenantKey  *ecdsa.PrivateKey
	tenantCert *x509.Certificate
	tenantDER  []byte

	tenantID string

	mu     sync.Mutex
	serial atomic.Int64
}

// NewTestCA creates a test PKI for the given tenant ID.
// The root CA signs the tenant CA which will sign all agent certs.
func NewTestCA(tenantID string) (*TestCA, error) {
	ca := &TestCA{tenantID: tenantID}
	ca.serial.Store(2)

	var err error
	ca.rootKey, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate root key: %w", err)
	}

	rootTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization:       []string{"Personel Test PKI"},
			OrganizationalUnit: []string{"Test Root CA"},
			CommonName:         fmt.Sprintf("Personel Test Root CA [%s]", tenantID),
		},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		MaxPathLen:            2,
	}

	ca.rootDER, err = x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, &ca.rootKey.PublicKey, ca.rootKey)
	if err != nil {
		return nil, fmt.Errorf("create root cert: %w", err)
	}
	ca.rootCert, err = x509.ParseCertificate(ca.rootDER)
	if err != nil {
		return nil, fmt.Errorf("parse root cert: %w", err)
	}

	// Tenant CA — signed by root.
	ca.tenantKey, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate tenant CA key: %w", err)
	}

	tenantTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			Organization:       []string{"Personel Test PKI"},
			OrganizationalUnit: []string{"Tenant CA"},
			CommonName:         fmt.Sprintf("Personel Test Tenant CA [%s]", tenantID),
		},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		MaxPathLen:            1,
	}

	ca.tenantDER, err = x509.CreateCertificate(rand.Reader, tenantTemplate, ca.rootCert, &ca.tenantKey.PublicKey, ca.rootKey)
	if err != nil {
		return nil, fmt.Errorf("create tenant cert: %w", err)
	}
	ca.tenantCert, err = x509.ParseCertificate(ca.tenantDER)
	if err != nil {
		return nil, fmt.Errorf("parse tenant cert: %w", err)
	}

	slog.Debug("test PKI initialized", "tenant_id", tenantID)
	return ca, nil
}

// AgentCert holds the TLS credentials for a single simulated agent.
type AgentCert struct {
	TLSCert    tls.Certificate
	X509Cert   *x509.Certificate
	EndpointID string
	CertDER    []byte
}

// IssueAgentCert creates a new per-endpoint client certificate signed by the
// tenant CA. The CN is set to "agent:<endpointID>" to match the gateway's
// expected SAN/CN format.
func (ca *TestCA) IssueAgentCert(endpointID string) (*AgentCert, error) {
	ca.mu.Lock()
	serial := ca.serial.Add(1)
	ca.mu.Unlock()

	agentKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate agent key for %s: %w", endpointID, err)
	}

	agentURI, _ := url.Parse(fmt.Sprintf("personel://tenant/%s/endpoint/%s", ca.tenantID, endpointID))

	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject: pkix.Name{
			Organization:       []string{"Personel Test PKI"},
			OrganizationalUnit: []string{"Agents"},
			CommonName:         fmt.Sprintf("agent:%s", endpointID),
		},
		NotBefore: time.Now().Add(-time.Minute),
		NotAfter:  time.Now().Add(14 * 24 * time.Hour), // matches production 14-day cert validity
		KeyUsage:  x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageClientAuth,
		},
		DNSNames:    []string{fmt.Sprintf("agent.%s.personel.test", endpointID)},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		URIs:        []*url.URL{agentURI},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, ca.tenantCert, &agentKey.PublicKey, ca.tenantKey)
	if err != nil {
		return nil, fmt.Errorf("sign agent cert for %s: %w", endpointID, err)
	}

	x509Cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, fmt.Errorf("parse agent cert for %s: %w", endpointID, err)
	}

	tlsCert := tls.Certificate{
		Certificate: [][]byte{certDER, ca.tenantDER, ca.rootDER},
		PrivateKey:  agentKey,
		Leaf:        x509Cert,
	}

	return &AgentCert{
		TLSCert:    tlsCert,
		X509Cert:   x509Cert,
		EndpointID: endpointID,
		CertDER:    certDER,
	}, nil
}

// ClientTLSConfig builds a *tls.Config for a simulated agent connecting to
// the gateway. It pins the tenant CA as the only trusted root, matching the
// agent-side cert pinning described in mtls-pki.md.
func (ca *TestCA) ClientTLSConfig(agentCert *AgentCert, gatewayHost string) *tls.Config {
	pool := x509.NewCertPool()
	pool.AddCert(ca.tenantCert)
	pool.AddCert(ca.rootCert)

	return &tls.Config{
		Certificates: []tls.Certificate{agentCert.TLSCert},
		RootCAs:      pool,
		ServerName:   gatewayHost,
		MinVersion:   tls.VersionTLS13,
	}
}

// ServerTLSConfig builds a *tls.Config for a mock gateway that requires
// client certs signed by this CA.
func (ca *TestCA) ServerTLSConfig(serverCert tls.Certificate) *tls.Config {
	pool := x509.NewCertPool()
	pool.AddCert(ca.tenantCert)
	pool.AddCert(ca.rootCert)

	return &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
	}
}

// IssueServerCert issues a TLS server cert for the gateway, signed by the
// tenant CA (matching the Personel cert hierarchy where the server intermediate
// is under the tenant CA).
func (ca *TestCA) IssueServerCert(dnsNames ...string) (tls.Certificate, error) {
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate server key: %w", err)
	}

	ca.mu.Lock()
	serial := ca.serial.Add(1)
	ca.mu.Unlock()

	allDNS := append([]string{"localhost", "gateway.personel.test"}, dnsNames...)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject: pkix.Name{
			Organization: []string{"Personel Test PKI"},
			CommonName:   "gateway.personel.test",
		},
		NotBefore:   time.Now().Add(-time.Minute),
		NotAfter:    time.Now().Add(90 * 24 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    allDNS,
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, ca.tenantCert, &serverKey.PublicKey, ca.tenantKey)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("sign server cert: %w", err)
	}

	return tls.Certificate{
		Certificate: [][]byte{certDER, ca.tenantDER},
		PrivateKey:  serverKey,
	}, nil
}

// TenantCACertPEM returns the tenant CA cert in PEM encoding for embedding
// in Docker compose env vars or config files.
func (ca *TestCA) TenantCACertPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.tenantDER})
}

// RootCACertPEM returns the root CA cert in PEM encoding.
func (ca *TestCA) RootCACertPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.rootDER})
}

// TenantID returns the tenant ID this CA was created for.
func (ca *TestCA) TenantID() string {
	return ca.tenantID
}

// SPKIPin computes the SPKI SHA-256 pin for the tenant CA, matching the
// agent-side cert pinning format described in mtls-pki.md §Certificate Pinning.
func (ca *TestCA) SPKIPin() ([32]byte, error) {
	spki, err := x509.MarshalPKIXPublicKey(ca.tenantCert.PublicKey)
	if err != nil {
		return [32]byte{}, fmt.Errorf("marshal SPKI: %w", err)
	}
	return sha256.Sum256(spki), nil
}
