//! Re-exports of all generated protobuf / gRPC stubs.
//!
//! All `personel.v1.*` proto types are accessible as `personel_proto::v1::*`.
//!
//! The generated code lives in `$OUT_DIR/personel.v1.rs` and is included via
//! the `include!` macro below. Do not hand-edit the generated files.

#![deny(unsafe_code)]
#![allow(clippy::pedantic)] // generated code is not pedantic-clean
#![allow(missing_docs)]     // generated code lacks rustdoc

/// All generated types for the `personel.v1` package.
pub mod v1 {
    // tonic_build places the generated code in a file named after the proto
    // package with '.' replaced by '/'. For `personel.v1` the file is
    // `personel.v1.rs` inside OUT_DIR.
    tonic::include_proto!("personel.v1");
}

/// Convenience re-export of the gRPC client for the `AgentService`.
pub use v1::agent_service_client::AgentServiceClient;
