// Package liveview also handles LiveKit token issuance for agent publish sessions.
// The gateway does NOT issue admin-side tokens (admin-api does that). It only
// creates short-lived agent-side tokens scoped to publish-only access.
package liveview

import (
	"fmt"
	"time"

	"github.com/personel/gateway/internal/config"
)

// TokenConfig holds the LiveKit API credentials.
type TokenConfig struct {
	ServerURL string
	APIKey    string
	APISecret string
}

// AgentToken represents a short-lived LiveKit JWT for an agent to publish
// its screen stream into a room. The actual JWT generation requires the
// livekit-server-sdk-go library which is not in our go.mod to avoid scope
// creep; the real implementation should replace the placeholder below.
type AgentToken struct {
	Token     string
	Room      string
	Identity  string
	ExpiresAt time.Time
}

// IssueAgentToken creates a publish-only LiveKit access token for the given
// endpoint and session. The token is scoped to the specific room and grants
// CanPublish=true, CanSubscribe=false (agent only streams, never watches).
//
// NOTE: Real implementation requires github.com/livekit/server-sdk-go.
// Add it to go.mod and replace this stub before pilot deploy.
func IssueAgentToken(cfg config.LiveViewConfig, endpointID, sessionID string) (*AgentToken, error) {
	if cfg.LiveKitAPIKey == "" || cfg.LiveKitAPISecret == "" {
		return nil, fmt.Errorf("liveview: LiveKit API credentials not configured")
	}

	room := fmt.Sprintf("lv-%s-%s", truncateID(endpointID, 8), truncateID(sessionID, 8))
	identity := fmt.Sprintf("agent-%s", endpointID)

	// TODO: replace with real livekit/server-sdk-go token generation:
	//   at := auth.NewAccessToken(cfg.LiveKitAPIKey, cfg.LiveKitAPISecret)
	//   grant := &auth.VideoGrant{RoomJoin: true, Room: room, CanPublish: true, CanSubscribe: false}
	//   at.AddGrant(grant).SetIdentity(identity).SetValidFor(5 * time.Minute)
	//   token, _ := at.ToJWT()

	return &AgentToken{
		Token:     fmt.Sprintf("placeholder-jwt-for-%s", identity),
		Room:      room,
		Identity:  identity,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}, nil
}

func truncateID(id string, n int) string {
	if len(id) <= n {
		return id
	}
	return id[:n]
}
