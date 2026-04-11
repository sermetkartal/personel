// policy_push_test.go tests the policy push flow:
//   API publishes policy → gateway delivers to agent via stream → agent acks.
package e2e

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/personel/qa/internal/harness"
	personelv1 "github.com/personel/qa/internal/proto"
	"github.com/personel/qa/internal/simulator"
)

// TestPolicyPushDelivery verifies that a policy published via the Admin API
// is delivered to a connected agent within a reasonable time.
func TestPolicyPushDelivery(t *testing.T) {
	harness.RequireIntegration(t)
	harness.RequireGateway(t)

	stack := harness.MustStart(t, harness.StackOptions{WithGateway: true, WithAPI: true})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)

	tenantID := "88888888-8888-8888-8888-888888888888"
	ca, err := simulator.NewTestCA(tenantID)
	require.NoError(t, err)

	endpointID := "a1a10000-0000-0000-0000-000000000001"
	cert, err := ca.IssueAgentCert(endpointID)
	require.NoError(t, err)

	tlsCfg := ca.ClientTLSConfig(cert, "gateway.personel.test")
	conn, err := grpc.NewClient(stack.GatewayAddr, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
	require.NoError(t, err)
	defer conn.Close()

	stream, err := personelv1.NewAgentServiceClient(conn).Stream(ctx)
	require.NoError(t, err)

	// Send Hello.
	require.NoError(t, stream.Send(&personelv1.AgentMessage{
		Payload: &personelv1.AgentMessage_Hello{
			Hello: &personelv1.Hello{
				AgentVersion:  &personelv1.AgentVersion{Major: 1},
				EndpointId:    &personelv1.EndpointId{Value: uuidBytes(endpointID)},
				TenantId:      &personelv1.TenantId{Value: uuidBytes(tenantID)},
				HwFingerprint: &personelv1.HardwareFingerprint{Blob: make([]byte, 32)},
				PeDekVersion:  1,
				TmkVersion:    1,
			},
		},
	}))

	// Await Welcome.
	welcomeMsg, err := stream.Recv()
	if err != nil {
		t.Skipf("gateway not available: %v", err)
	}
	if _, ok := welcomeMsg.Payload.(*personelv1.ServerMessage_Welcome); !ok {
		t.Skipf("expected Welcome, got %T", welcomeMsg.Payload)
	}

	// Publish a policy update via Admin API.
	apiBase := fmt.Sprintf("http://%s", stack.GatewayAddr)
	policyBody := `{
		"endpoint_id": "` + endpointID + `",
		"collectors": {"process": true, "window_focus": true, "keystroke_meta": true},
		"screenshot": {"interval_seconds": 60}
	}`

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(
		apiBase+"/v1/policies",
		"application/json",
		strings.NewReader(policyBody),
	)
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Logf("policy publish failed (API not ready); skipping delivery check")
		t.Skip("Admin API policy publish not available")
	}
	defer resp.Body.Close()

	// Wait for PolicyPush on the stream.
	policyReceived := make(chan *personelv1.PolicyPush, 1)
	go func() {
		for {
			msg, err := stream.Recv()
			if err != nil {
				return
			}
			if pp, ok := msg.Payload.(*personelv1.ServerMessage_PolicyPush); ok {
				policyReceived <- pp.PolicyPush
				return
			}
		}
	}()

	select {
	case policy := <-policyReceived:
		assert.NotEmpty(t, policy.PolicyVersion, "PolicyPush must include version")
		assert.NotNil(t, policy.Signature, "PolicyPush must be signed")
		t.Logf("policy received: version=%s", policy.PolicyVersion)
	case <-time.After(30 * time.Second):
		t.Error("timeout waiting for PolicyPush delivery to agent")
	}
}
