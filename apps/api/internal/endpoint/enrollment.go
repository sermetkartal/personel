// Package endpoint — enrollment helpers (Vault AppRole single-use Secret ID flow).
// The actual logic lives in Service.Enroll; this file documents the contract.
//
// Per vault-setup.md §5.1:
//   vault write auth/approle/role/agent-enrollment secret_id_num_uses=1 secret_id_ttl=15m token_ttl=15m
//
// The Admin API calls IssueEnrollmentSecretID for each new enrollment.
// The token is single-use and expires in 15 minutes.
// The Secret ID is delivered to the operator via the enrollment response.
// The operator passes it to the agent installer, which uses it exactly once.
package endpoint
