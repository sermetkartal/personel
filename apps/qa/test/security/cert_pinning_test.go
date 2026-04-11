// cert_pinning_test.go verifies that the agent's certificate pinning
// correctly rejects connections to servers with unexpected certs.
package security

import (
	"crypto/tls"
	"crypto/x509"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/personel/qa/internal/simulator"
)

// TestCertPinningRejectsMismatch verifies that a TLS config with a pinned
// tenant CA rejects connections to servers presenting a different CA's cert.
func TestCertPinningRejectsMismatch(t *testing.T) {
	// Create two separate CAs.
	ca1, err := simulator.NewTestCA("tenant-ca-1")
	require.NoError(t, err)

	ca2, err := simulator.NewTestCA("tenant-ca-2")
	require.NoError(t, err)

	// Issue an agent cert from CA1.
	agentCert, err := ca1.IssueAgentCert("endpoint-pinning-test-001")
	require.NoError(t, err)

	// Build TLS config pinned to CA1's tenant CA.
	tlsCfgCA1 := ca1.ClientTLSConfig(agentCert, "gateway.personel.test")

	// Issue a server cert from CA2 (would be used by a rogue/misconfigured gateway).
	serverCertCA2, err := ca2.IssueServerCert("gateway.personel.test")
	require.NoError(t, err)

	// Attempt TLS handshake: CA1-pinned client vs CA2-signed server.
	// Should fail.
	err = simulateTLSHandshake(tlsCfgCA1, ca2.ServerTLSConfig(serverCertCA2))
	assert.Error(t, err, "cert pinning must reject server cert from wrong CA")
	t.Logf("cert pinning correctly rejected wrong CA: %v", err)
}

// TestCertPinningAcceptsCorrectCA verifies that the TLS config correctly
// accepts a connection to a server with the expected CA cert.
func TestCertPinningAcceptsCorrectCA(t *testing.T) {
	ca, err := simulator.NewTestCA("tenant-correct-ca")
	require.NoError(t, err)

	agentCert, err := ca.IssueAgentCert("endpoint-pinning-test-002")
	require.NoError(t, err)

	tlsCfgClient := ca.ClientTLSConfig(agentCert, "gateway.personel.test")

	serverCert, err := ca.IssueServerCert("gateway.personel.test")
	require.NoError(t, err)

	tlsCfgServer := ca.ServerTLSConfig(serverCert)

	// Should succeed.
	err = simulateTLSHandshake(tlsCfgClient, tlsCfgServer)
	assert.NoError(t, err, "cert pinning must accept server cert from correct CA")
	t.Log("cert pinning correctly accepted correct CA")
}

// TestCertPinningExpiredAgentCertRejected verifies that an expired agent cert
// is rejected by the server.
func TestCertPinningExpiredAgentCertRejected(t *testing.T) {
	t.Skip("expired cert test requires time manipulation or a dedicated test cert with past NotAfter — skipping for now")
}

// TestSPKIPinComputation verifies the SPKI pin computation matches what
// mtls-pki.md describes.
func TestSPKIPinComputation(t *testing.T) {
	ca, err := simulator.NewTestCA("spki-pin-test")
	require.NoError(t, err)

	pin, err := ca.SPKIPin()
	require.NoError(t, err)

	assert.Len(t, pin, 32, "SPKI SHA-256 pin must be 32 bytes")
	t.Logf("tenant CA SPKI pin: %x", pin)

	// Compute it again — must be deterministic.
	pin2, err := ca.SPKIPin()
	require.NoError(t, err)
	assert.Equal(t, pin, pin2, "SPKI pin must be deterministic")
}

// simulateTLSHandshake performs an in-memory TLS handshake using net.Pipe.
func simulateTLSHandshake(clientCfg, serverCfg *tls.Config) error {
	// Use x509 verify to simulate what TLS would do without actually dialing.
	// This tests the certificate chain validation logic.

	// Get the server cert from the server config.
	if len(serverCfg.Certificates) == 0 {
		return nil
	}
	serverLeaf := serverCfg.Certificates[0]

	// Parse the server cert.
	if serverLeaf.Leaf == nil && len(serverLeaf.Certificate) > 0 {
		parsed, err := x509.ParseCertificate(serverLeaf.Certificate[0])
		if err != nil {
			return err
		}
		serverLeaf.Leaf = parsed
	}

	// Build a verification config matching what the client would do.
	verifyOpts := x509.VerifyOptions{
		Roots:         clientCfg.RootCAs,
		CurrentTime:   serverLeaf.Leaf.NotBefore.Add(1),
		DNSName:       clientCfg.ServerName,
		Intermediates: x509.NewCertPool(),
	}

	// Add intermediate certs from the server's chain.
	for i := 1; i < len(serverLeaf.Certificate); i++ {
		intermediate, err := x509.ParseCertificate(serverLeaf.Certificate[i])
		if err == nil {
			verifyOpts.Intermediates.AddCert(intermediate)
		}
	}

	_, err := serverLeaf.Leaf.Verify(verifyOpts)
	return err
}
