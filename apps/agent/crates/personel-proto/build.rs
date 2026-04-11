//! build.rs: invoke tonic-build to generate Rust code from all .proto files
//! under `proto/personel/v1/`.
//!
//! Proto files are read from the monorepo root, two levels above the workspace.
//! The generated code is placed in `$OUT_DIR` and re-exported from `src/lib.rs`.

use std::path::PathBuf;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Locate proto root relative to this build script.
    // Workspace root is: apps/agent/
    // Monorepo root is:  ../../  (two levels up from apps/agent/)
    let manifest_dir = PathBuf::from(std::env::var("CARGO_MANIFEST_DIR")?);
    let proto_root = manifest_dir
        .join("../../../../proto")
        .canonicalize()
        .unwrap_or_else(|_| manifest_dir.join("../../../../proto"));

    let proto_files = [
        proto_root.join("personel/v1/common.proto"),
        proto_root.join("personel/v1/events.proto"),
        proto_root.join("personel/v1/policy.proto"),
        proto_root.join("personel/v1/live_view.proto"),
        proto_root.join("personel/v1/agent.proto"),
    ];

    // Emit rebuild-if-changed directives so cargo re-runs build.rs on proto
    // modifications.
    for f in &proto_files {
        println!("cargo:rerun-if-changed={}", f.display());
    }
    println!("cargo:rerun-if-changed=build.rs");

    tonic_build::configure()
        // Build client stubs only — the agent is a gRPC client, not a server.
        .build_client(true)
        .build_server(false)
        .compile(
            &proto_files,
            &[&proto_root],
        )?;

    Ok(())
}
