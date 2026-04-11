// Package proto provides the compiled protobuf types for the Personel v1 API.
//
// The types in this package are generated from proto/personel/v1/*.proto.
// Because the proto compilation step requires protoc, this package contains
// hand-written stubs that mirror the generated code structure. When the real
// proto generation is wired up in CI, this package will be replaced by the
// generated output.
//
// All type names and field names match the generated Go struct names exactly
// as they would appear from protoc-gen-go with option go_package =
// "github.com/personel/proto/personel/v1;personelv1".
//
// Usage in the QA framework:
//   - Simulator uses AgentServiceClient to connect to the gateway.
//   - Tests import this package for type assertions on proto messages.
//   - The fuzz test in test/security/fuzz uses the proto parsing functions.
package proto
