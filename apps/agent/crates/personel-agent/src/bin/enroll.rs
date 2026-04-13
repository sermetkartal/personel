//! `enroll` — one-shot enrollment CLI.
//!
//! Called by the MSI installer immediately after installation:
//! ```text
//! enroll --token <enrollment-token> --gateway https://gw.personel.example:9443
//! ```
//!
//! The `--token` is a base64url-no-pad JSON blob produced by the admin API
//! `/v1/endpoints/enroll` endpoint:
//! ```json
//! { "role_id": "...", "secret_id": "...", "enroll_url": "https://.../v1/agent-enroll" }
//! ```
//!
//! Steps performed:
//! 1. Parse --token, base64url-decode, extract role_id + secret_id + enroll_url
//! 2. Generate ECDSA P-256 keypair via rcgen and build a PKCS#10 CSR (CN = hostname)
//! 3. Compute hardware fingerprint = SHA-256(MachineGuid + hostname) on Windows,
//!    random UUID on dev builds
//! 4. POST {role_id, secret_id, csr_pem, hw_fingerprint, hostname, os_version,
//!    agent_version} to enroll_url
//! 5. Receive {endpoint_id, tenant_id, cert_pem, chain_pem, gateway_url,
//!    spki_pin_sha256, serial_number, not_after}
//! 6. Write cert.pem (leaf+chain), private_key.enc (DPAPI-sealed PKCS#8 DER),
//!    config.toml to the data dir.

use anyhow::{anyhow, Context, Result};
use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use base64::Engine as _;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::fs;
use std::path::PathBuf;
use std::process::ExitCode;

/// Returns the default data directory (duplicated here because bin/ targets
/// cannot import from the sibling src/ module tree in the same crate without
/// a lib target; keep in sync with `config::default_data_dir`).
fn default_data_dir() -> PathBuf {
    #[cfg(target_os = "windows")]
    {
        PathBuf::from(r"C:\ProgramData\Personel\agent")
    }
    #[cfg(not(target_os = "windows"))]
    {
        std::env::current_dir()
            .unwrap_or_else(|_| PathBuf::from("/tmp/personel-agent"))
            .join("personel-agent-data")
    }
}

#[derive(Debug, Deserialize)]
struct EnrollmentToken {
    role_id: String,
    secret_id: String,
    enroll_url: String,
}

#[derive(Debug, Serialize)]
struct EnrollRequest {
    role_id: String,
    secret_id: String,
    csr_pem: String,
    hw_fingerprint: String,
    hostname: String,
    os_version: String,
    agent_version: String,
}

#[derive(Debug, Deserialize)]
struct EnrollResponse {
    endpoint_id: String,
    tenant_id: String,
    cert_pem: String,
    chain_pem: String,
    gateway_url: String,
    spki_pin_sha256: String,
    #[allow(dead_code)]
    serial_number: String,
    #[allow(dead_code)]
    not_after: String,
}

fn main() -> ExitCode {
    tracing_subscriber::fmt().with_env_filter("info").init();

    match run() {
        Ok(()) => ExitCode::SUCCESS,
        Err(EnrollErr::BadToken(msg)) => {
            eprintln!("error: invalid token: {msg}");
            ExitCode::from(2)
        }
        Err(EnrollErr::Http(msg)) => {
            eprintln!("error: enrollment request failed: {msg}");
            ExitCode::from(3)
        }
        Err(EnrollErr::Io(msg)) => {
            eprintln!("error: data dir write failed: {msg}");
            ExitCode::from(4)
        }
        Err(EnrollErr::Other(e)) => {
            eprintln!("error: {e:#}");
            ExitCode::from(1)
        }
    }
}

#[derive(Debug)]
enum EnrollErr {
    BadToken(String),
    Http(String),
    Io(String),
    Other(anyhow::Error),
}

impl From<anyhow::Error> for EnrollErr {
    fn from(e: anyhow::Error) -> Self {
        EnrollErr::Other(e)
    }
}

fn run() -> std::result::Result<(), EnrollErr> {
    let args: Vec<String> = std::env::args().collect();

    let token_arg = get_arg(&args, "--token")
        .ok_or_else(|| EnrollErr::BadToken("--token is required".into()))?;
    // --gateway is accepted for forward-compat with the MSI invocation, but the
    // authoritative gateway URL comes back in the enrollment response. We log
    // the supplied value as a hint only.
    let supplied_gateway = get_arg(&args, "--gateway").unwrap_or("");
    let data_dir = default_data_dir();

    tracing::info!(supplied_gateway, data_dir = %data_dir.display(), "starting enrollment");

    // 1. Parse + decode token.
    let token_bytes = URL_SAFE_NO_PAD
        .decode(token_arg.as_bytes())
        .map_err(|e| EnrollErr::BadToken(format!("base64 decode: {e}")))?;
    let token: EnrollmentToken = serde_json::from_slice(&token_bytes)
        .map_err(|e| EnrollErr::BadToken(format!("json parse: {e}")))?;
    if token.role_id.is_empty() || token.secret_id.is_empty() || token.enroll_url.is_empty() {
        return Err(EnrollErr::BadToken(
            "role_id, secret_id, and enroll_url are all required".into(),
        ));
    }

    // 2. Generate ECDSA P-256 keypair + CSR.
    let host = hostname::get()
        .map(|h| h.to_string_lossy().into_owned())
        .unwrap_or_else(|_| "unknown-host".to_string());
    let (csr_pem, private_key_der) =
        generate_csr(&host).context("generate CSR").map_err(EnrollErr::Other)?;

    // 3. Hardware fingerprint.
    let hw_fingerprint = hw_fingerprint(&host);

    // 4. Build + send request.
    let os_version = os_version_string();
    let req = EnrollRequest {
        role_id: token.role_id.clone(),
        secret_id: token.secret_id.clone(),
        csr_pem: csr_pem.clone(),
        hw_fingerprint,
        hostname: host.clone(),
        os_version,
        agent_version: env!("CARGO_PKG_VERSION").to_string(),
    };

    let client = reqwest::blocking::Client::builder()
        .danger_accept_invalid_certs(true)
        .timeout(std::time::Duration::from_secs(30))
        .build()
        .map_err(|e| EnrollErr::Http(format!("client build: {e}")))?;

    tracing::info!(url = %token.enroll_url, "POST enroll request");
    let resp = client
        .post(&token.enroll_url)
        .json(&req)
        .send()
        .map_err(|e| EnrollErr::Http(format!("send: {e}")))?;

    let status = resp.status();
    if !status.is_success() {
        let body = resp.text().unwrap_or_default();
        return Err(EnrollErr::Http(format!("status {status}: {body}")));
    }
    let body: EnrollResponse = resp
        .json()
        .map_err(|e| EnrollErr::Http(format!("decode response: {e}")))?;

    // 5. Persist material.
    fs::create_dir_all(&data_dir)
        .map_err(|e| EnrollErr::Io(format!("create_dir_all {}: {e}", data_dir.display())))?;

    let cert_path = data_dir.join("cert.pem");
    let key_path = data_dir.join("private_key.enc");
    let root_ca_path = data_dir.join("root_ca.pem");
    let cfg_path = data_dir.join("config.toml");

    // cert.pem holds leaf || chain (tonic Identity::from_pem accepts either
    // the leaf alone or a leaf+intermediates bundle).
    let mut cert_bundle = body.cert_pem.clone();
    if !cert_bundle.ends_with('\n') {
        cert_bundle.push('\n');
    }
    cert_bundle.push_str(&body.chain_pem);
    if !cert_bundle.ends_with('\n') {
        cert_bundle.push('\n');
    }
    fs::write(&cert_path, cert_bundle.as_bytes())
        .map_err(|e| EnrollErr::Io(format!("write cert: {e}")))?;

    // root_ca.pem is the trust anchor used by the agent's mTLS client to
    // verify the gateway's server certificate. For a flat Vault PKI this is
    // the issuing CA itself; for chained PKIs the response includes the full
    // chain and the root is the last cert in the bundle. Either way, writing
    // chain_pem here gives rustls the anchors it needs.
    let mut root_bundle = body.chain_pem.clone();
    if !root_bundle.ends_with('\n') {
        root_bundle.push('\n');
    }
    fs::write(&root_ca_path, root_bundle.as_bytes())
        .map_err(|e| EnrollErr::Io(format!("write root ca: {e}")))?;

    let sealed = seal_private_key(&private_key_der);
    fs::write(&key_path, &sealed)
        .map_err(|e| EnrollErr::Io(format!("write key: {e}")))?;

    let cfg = format!(
        concat!(
            "# Generated by enroll.exe — do not edit by hand.\n",
            "# Schema mirrors personel_agent::config::AgentConfig.\n",
            "\n",
            "[enrollment]\n",
            "tenant_id    = \"{}\"\n",
            "endpoint_id  = \"{}\"\n",
            "gateway_url  = \"{}\"\n",
            "spki_pins    = [\"{}\"]\n",
            "cert_path    = \"{}\"\n",
            "key_path     = \"{}\"\n",
            "root_ca_path = \"{}\"\n",
        ),
        body.tenant_id,
        body.endpoint_id,
        body.gateway_url,
        body.spki_pin_sha256,
        cert_path.display().to_string().replace('\\', "\\\\"),
        key_path.display().to_string().replace('\\', "\\\\"),
        root_ca_path.display().to_string().replace('\\', "\\\\"),
    );
    fs::write(&cfg_path, cfg.as_bytes())
        .map_err(|e| EnrollErr::Io(format!("write config: {e}")))?;

    println!("enrollment complete: endpoint_id={}", body.endpoint_id);
    Ok(())
}

fn get_arg<'a>(args: &'a [String], flag: &str) -> Option<&'a str> {
    args.windows(2)
        .find(|w| w[0] == flag)
        .map(|w| w[1].as_str())
}

/// Generates an ECDSA P-256 keypair + PKCS#10 CSR via rcgen.
/// Returns (csr_pem, pkcs8_der_private_key).
fn generate_csr(host: &str) -> Result<(String, Vec<u8>)> {
    let mut params = rcgen::CertificateParams::new(vec![host.to_string()]);
    params.alg = &rcgen::PKCS_ECDSA_P256_SHA256;
    let mut dn = rcgen::DistinguishedName::new();
    dn.push(rcgen::DnType::CommonName, host.to_string());
    params.distinguished_name = dn;

    let cert = rcgen::Certificate::from_params(params)
        .map_err(|e| anyhow!("rcgen Certificate::from_params: {e}"))?;
    let csr_pem = cert
        .serialize_request_pem()
        .map_err(|e| anyhow!("rcgen serialize_request_pem: {e}"))?;
    let key_der = cert.serialize_private_key_der();
    Ok((csr_pem, key_der))
}

#[cfg(target_os = "windows")]
fn seal_private_key(plaintext: &[u8]) -> Vec<u8> {
    match personel_os::windows::dpapi::protect(plaintext) {
        Ok(blob) => blob,
        Err(e) => {
            tracing::warn!(error = %e, "DPAPI protect failed; storing private key UNENCRYPTED — TODO");
            plaintext.to_vec()
        }
    }
}

#[cfg(not(target_os = "windows"))]
fn seal_private_key(plaintext: &[u8]) -> Vec<u8> {
    tracing::warn!("non-Windows build: storing private key UNENCRYPTED (DPAPI unavailable) — TODO");
    plaintext.to_vec()
}

/// Hardware fingerprint = lowercase hex SHA-256(MachineGuid || ":" || hostname).
/// On non-Windows dev builds, falls back to SHA-256("dev:" || random UUID).
fn hw_fingerprint(host: &str) -> String {
    let mut hasher = Sha256::new();
    #[cfg(target_os = "windows")]
    {
        match read_machine_guid() {
            Ok(guid) => {
                hasher.update(guid.as_bytes());
                hasher.update(b":");
                hasher.update(host.as_bytes());
            }
            Err(e) => {
                tracing::warn!(error = %e, "MachineGuid read failed; using random fallback");
                hasher.update(b"dev:");
                hasher.update(uuid::Uuid::new_v4().as_bytes());
                hasher.update(host.as_bytes());
            }
        }
    }
    #[cfg(not(target_os = "windows"))]
    {
        hasher.update(b"dev:");
        hasher.update(uuid::Uuid::new_v4().as_bytes());
        hasher.update(host.as_bytes());
    }
    hex::encode(hasher.finalize())
}

#[cfg(target_os = "windows")]
fn read_machine_guid() -> Result<String> {
    use windows::core::PCWSTR;
    use windows::Win32::System::Registry::{
        RegCloseKey, RegOpenKeyExW, RegQueryValueExW, HKEY, HKEY_LOCAL_MACHINE, KEY_READ,
        REG_VALUE_TYPE,
    };

    let subkey: Vec<u16> = "SOFTWARE\\Microsoft\\Cryptography\\"
        .encode_utf16()
        .chain(std::iter::once(0))
        .collect();
    let value: Vec<u16> = "MachineGuid\0".encode_utf16().collect();

    // SAFETY: All Win32 inputs are well-formed UTF-16 with NUL terminators;
    // output buffers are sized correctly and the key is closed before return.
    unsafe {
        let mut hkey = HKEY::default();
        RegOpenKeyExW(
            HKEY_LOCAL_MACHINE,
            PCWSTR(subkey.as_ptr()),
            0,
            KEY_READ,
            &mut hkey,
        )
        .ok()
        .map_err(|e| anyhow!("RegOpenKeyExW: {e:?}"))?;

        let mut buf = vec![0u16; 64];
        let mut cb: u32 = (buf.len() * 2) as u32;
        let mut ty = REG_VALUE_TYPE::default();
        let res = RegQueryValueExW(
            hkey,
            PCWSTR(value.as_ptr()),
            None,
            Some(&mut ty),
            Some(buf.as_mut_ptr() as *mut u8),
            Some(&mut cb),
        );
        let _ = RegCloseKey(hkey);
        res.ok().map_err(|e| anyhow!("RegQueryValueExW: {e:?}"))?;

        // cb is in bytes; trim trailing NUL u16.
        let chars = (cb as usize) / 2;
        let end = buf[..chars]
            .iter()
            .position(|&c| c == 0)
            .unwrap_or(chars);
        Ok(String::from_utf16_lossy(&buf[..end]))
    }
}

fn os_version_string() -> String {
    #[cfg(target_os = "windows")]
    {
        // Best-effort: read CurrentVersion ProductName + CurrentBuild from
        // HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion. We don't fail
        // enrollment on missing values — return whatever we can.
        let product = read_winnt_string("ProductName").unwrap_or_else(|| "Windows".into());
        let build = read_winnt_string("CurrentBuild").unwrap_or_default();
        if build.is_empty() {
            product
        } else {
            format!("{product} (build {build})")
        }
    }
    #[cfg(not(target_os = "windows"))]
    {
        format!("{} (dev)", std::env::consts::OS)
    }
}

#[cfg(target_os = "windows")]
fn read_winnt_string(value_name: &str) -> Option<String> {
    use windows::core::PCWSTR;
    use windows::Win32::System::Registry::{
        RegCloseKey, RegOpenKeyExW, RegQueryValueExW, HKEY, HKEY_LOCAL_MACHINE, KEY_READ,
        REG_VALUE_TYPE,
    };

    let subkey: Vec<u16> = "SOFTWARE\\Microsoft\\Windows NT\\CurrentVersion\0"
        .encode_utf16()
        .collect();
    let value: Vec<u16> = value_name
        .encode_utf16()
        .chain(std::iter::once(0))
        .collect();

    // SAFETY: well-formed wide strings, sized output buffer, key always closed.
    unsafe {
        let mut hkey = HKEY::default();
        RegOpenKeyExW(
            HKEY_LOCAL_MACHINE,
            PCWSTR(subkey.as_ptr()),
            0,
            KEY_READ,
            &mut hkey,
        )
        .ok()
        .ok()?;

        let mut buf = vec![0u16; 256];
        let mut cb: u32 = (buf.len() * 2) as u32;
        let mut ty = REG_VALUE_TYPE::default();
        let res = RegQueryValueExW(
            hkey,
            PCWSTR(value.as_ptr()),
            None,
            Some(&mut ty),
            Some(buf.as_mut_ptr() as *mut u8),
            Some(&mut cb),
        );
        let _ = RegCloseKey(hkey);
        res.ok().ok()?;
        let chars = (cb as usize) / 2;
        let end = buf[..chars]
            .iter()
            .position(|&c| c == 0)
            .unwrap_or(chars);
        Some(String::from_utf16_lossy(&buf[..end]))
    }
}

