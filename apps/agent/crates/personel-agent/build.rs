//! build.rs for personel-agent.
//!
//! Proto code generation happens in `personel-proto/build.rs`. This build
//! script is kept minimal but reserved for future use (e.g., embedding build
//! metadata, version strings, or obfuscated constants).

fn main() {
    // Emit the git commit hash as a compile-time env var for embedding in the
    // agent version string.
    //
    // `VERGEN_GIT_SHA` would be provided by the `vergen` crate in a future
    // hardening sprint. For now we embed a placeholder.
    println!("cargo:rustc-env=AGENT_GIT_SHA=dev");
    println!("cargo:rerun-if-changed=build.rs");
}
