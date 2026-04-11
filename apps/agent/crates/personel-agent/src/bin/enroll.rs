//! `enroll` — one-shot enrollment CLI.
//!
//! Called by the MSI installer immediately after installation:
//! ```text
//! enroll --token <enrollment-token> --gateway https://gw.personel.example:443
//! ```
//!
//! Steps:
//! 1. Generate X25519 enrollment key pair on this host.
//! 2. Generate RSA/ECDSA CSR (private key stays on host, sealed by DPAPI).
//! 3. POST to `GET /v1/enroll` with token + hw_fingerprint + CSR public key.
//! 4. Receive signed cert + chain + tenant CA pin + PE-DEK sealed envelope.
//! 5. Store cert, key, PE-DEK, and config to the data directory.
//! 6. Write `config.toml` with tenant_id, endpoint_id, gateway_url, spki_pins.

use anyhow::{Context, Result};
use std::path::PathBuf;

/// Returns the default data directory (duplicated here because bin/ targets
/// cannot import from the sibling src/ module tree in the same crate without
/// a lib target; keep in sync with `config::default_data_dir`).
fn default_data_dir() -> PathBuf {
    #[cfg(target_os = "windows")]
    { PathBuf::from(r"C:\ProgramData\Personel\agent") }
    #[cfg(not(target_os = "windows"))]
    {
        std::env::current_dir()
            .unwrap_or_else(|_| PathBuf::from("/tmp/personel-agent"))
            .join("personel-agent-data")
    }
}

fn main() -> Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter("info")
        .init();

    let args: Vec<String> = std::env::args().collect();

    let token = get_arg(&args, "--token").context("--token is required")?;
    let gateway = get_arg(&args, "--gateway").context("--gateway is required")?;
    let data_dir = default_data_dir();

    tracing::info!(%gateway, data_dir = %data_dir.display(), "starting enrollment");

    // TODO: implement enrollment steps 1-6 described above.
    // This requires:
    // - personel_crypto::enroll::generate_enrollment_keypair()
    // - Generate PKCS#10 CSR (via `rcgen` crate)
    // - personel_os::dpapi::protect() to seal the private key
    // - HTTP POST to gateway /v1/enroll
    // - personel_crypto::enroll::unseal_dek() with the returned SealedDek
    // - Write all material to data_dir

    tracing::error!("enrollment not yet implemented");
    std::process::exit(1);
}

fn get_arg<'a>(args: &'a [String], flag: &str) -> Option<&'a str> {
    args.windows(2)
        .find(|w| w[0] == flag)
        .map(|w| w[1].as_str())
}
