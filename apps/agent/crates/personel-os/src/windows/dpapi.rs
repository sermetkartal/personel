//! Safe wrapper around DPAPI `CryptProtectData` / `CryptUnprotectData`.
//!
//! Uses machine-scope protection (`CRYPTPROTECT_LOCAL_MACHINE`) so the sealed
//! blob can only be unsealed on the same machine account.

use windows::Win32::Security::Cryptography::{
    CryptProtectData, CryptUnprotectData, CRYPTOAPI_BLOB,
    CRYPTPROTECT_LOCAL_MACHINE, CRYPTPROTECT_UI_FORBIDDEN,
};
use windows::Win32::Foundation::LocalFree;

use personel_core::error::{AgentError, Result};
use zeroize::Zeroizing;

/// Seals `plaintext` using DPAPI with machine-scope protection.
///
/// The returned `Vec<u8>` is the opaque DPAPI blob suitable for storage in
/// the registry or a config file.
///
/// # Errors
///
/// Returns [`AgentError::Dpapi`] if `CryptProtectData` fails.
pub fn protect(plaintext: &[u8]) -> Result<Vec<u8>> {
    // SAFETY: We carefully initialise all CRYPTOAPI_BLOB fields and check the
    // return value. The `LocalFree` on the output blob is called before the
    // function returns, preventing a leak.
    unsafe {
        let mut input_blob = CRYPTOAPI_BLOB {
            cbData: plaintext.len() as u32,
            pbData: plaintext.as_ptr() as *mut u8,
        };
        let mut output_blob = CRYPTOAPI_BLOB { cbData: 0, pbData: std::ptr::null_mut() };

        let flags = CRYPTPROTECT_LOCAL_MACHINE | CRYPTPROTECT_UI_FORBIDDEN;
        let ok = CryptProtectData(
            &mut input_blob,
            None,             // description
            None,             // optional entropy
            None,             // reserved
            None,             // prompt struct
            flags,
            &mut output_blob,
        );

        if !ok.as_bool() {
            return Err(AgentError::Dpapi(format!(
                "CryptProtectData failed: {:?}",
                windows::core::Error::from_win32()
            )));
        }

        // Copy data out before freeing.
        let sealed = std::slice::from_raw_parts(output_blob.pbData, output_blob.cbData as usize)
            .to_vec();
        // Free the DPAPI-allocated buffer.
        LocalFree(windows::Win32::Foundation::HLOCAL(output_blob.pbData as _));

        Ok(sealed)
    }
}

/// Unseals a DPAPI blob and returns the plaintext.
///
/// # Errors
///
/// Returns [`AgentError::Dpapi`] if `CryptUnprotectData` fails (wrong
/// machine, blob corrupt, or TPM mismatch).
pub fn unprotect(sealed: &[u8]) -> Result<Zeroizing<Vec<u8>>> {
    // SAFETY: Same pattern as `protect`. Output buffer is freed via LocalFree.
    unsafe {
        let mut input_blob = CRYPTOAPI_BLOB {
            cbData: sealed.len() as u32,
            pbData: sealed.as_ptr() as *mut u8,
        };
        let mut output_blob = CRYPTOAPI_BLOB { cbData: 0, pbData: std::ptr::null_mut() };

        let ok = CryptUnprotectData(
            &mut input_blob,
            None,             // description out
            None,             // optional entropy
            None,             // reserved
            None,             // prompt struct
            CRYPTPROTECT_UI_FORBIDDEN,
            &mut output_blob,
        );

        if !ok.as_bool() {
            return Err(AgentError::Dpapi(format!(
                "CryptUnprotectData failed: {:?}",
                windows::core::Error::from_win32()
            )));
        }

        let plaintext = Zeroizing::new(
            std::slice::from_raw_parts(output_blob.pbData, output_blob.cbData as usize).to_vec(),
        );
        LocalFree(windows::Win32::Foundation::HLOCAL(output_blob.pbData as _));

        Ok(plaintext)
    }
}
