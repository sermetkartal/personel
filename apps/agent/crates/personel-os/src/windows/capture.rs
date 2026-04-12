//! DXGI Desktop Duplication screen capture.
//!
//! Implements frame grabbing via the Windows Desktop Duplication API
//! (requires Windows 8+). The captured BGRA frame is JPEG-encoded using the
//! `image` crate (jpeg feature only, no PNG/BMP overhead).
//!
//! # Windows APIs used
//!
//! - `D3D11CreateDevice` — creates a D3D11 hardware device on the primary
//!   adapter.
//! - `IDXGIDevice::GetAdapter` — obtains the DXGI adapter from the device.
//! - `IDXGIAdapter::EnumOutputs` — enumerates display outputs (monitors).
//! - `IDXGIOutput::QueryInterface::<IDXGIOutput1>` — upgrades output to v1
//!   which exposes `DuplicateOutput`.
//! - `IDXGIOutput1::DuplicateOutput` — opens the `IDXGIOutputDuplication`
//!   session for the monitor.
//! - `IDXGIOutputDuplication::AcquireNextFrame` — blocks up to the timeout
//!   and returns the current desktop frame.
//! - `IDXGIOutputDuplication::ReleaseFrame` — releases the acquired frame
//!   (always called, even on error).
//! - `ID3D11Device::CreateTexture2D` — allocates a CPU-readable staging
//!   texture.
//! - `ID3D11DeviceContext::CopyResource` — copies the GPU frame to staging.
//! - `ID3D11DeviceContext::Map` / `Unmap` — CPU-side BGRA read-back.
//! - `IDXGIOutput::GetDesc` — retrieves the monitor resolution.
//!
//! # Phase 2 TODO
//!
//! - Multi-monitor support: loop `IDXGIAdapter::EnumOutputs` and expose one
//!   `DxgiCapture` per monitor index.
//! - `ScreenClipCapture`: DXGI frame loop → H.264 encode via Media Foundation
//!   Transform for ≤30 s clips.

use image::codecs::jpeg::JpegEncoder;
use image::ColorType;
use windows::Win32::Graphics::Direct3D::D3D_DRIVER_TYPE_HARDWARE;
use windows::Win32::Graphics::Direct3D11::{
    D3D11CreateDevice, ID3D11Device, ID3D11DeviceContext, ID3D11Texture2D, D3D11_BIND_FLAG,
    D3D11_CPU_ACCESS_READ, D3D11_MAP_READ, D3D11_MAPPED_SUBRESOURCE, D3D11_SDK_VERSION,
    D3D11_TEXTURE2D_DESC, D3D11_USAGE_STAGING,
};
use windows::Win32::Graphics::Dxgi::Common::{DXGI_FORMAT_B8G8R8A8_UNORM, DXGI_SAMPLE_DESC};
use windows::Win32::Graphics::Dxgi::{
    IDXGIDevice, IDXGIOutput, IDXGIOutput1, IDXGIOutputDuplication, DXGI_OUTDUPL_FRAME_INFO,
    DXGI_OUTPUT_DESC,
};

use personel_core::error::{AgentError, Result};

// ──────────────────────────────────────────────────────────────────────────────
// DXGI HRESULT constants
// ──────────────────────────────────────────────────────────────────────────────

const DXGI_ERROR_WAIT_TIMEOUT: i32 = 0x887A_0027_u32 as i32;
const DXGI_ERROR_ACCESS_LOST: i32 = 0x887A_0026_u32 as i32;
const DXGI_ERROR_DEVICE_REMOVED: i32 = 0x887A_0005_u32 as i32;

// ──────────────────────────────────────────────────────────────────────────────
// CaptureError
// ──────────────────────────────────────────────────────────────────────────────

/// Errors emitted by `DxgiCapture`. Mapped to `AgentError` by the collector.
#[derive(Debug, thiserror::Error)]
pub enum CaptureError {
    /// No new frame was ready within the 100 ms acquire timeout. Retry.
    #[error("frame timeout")]
    FrameTimeout,
    /// The duplication session was invalidated (resolution change, DWM reset,
    /// remote desktop). Call `reopen()` before the next `grab_frame`.
    #[error("access lost — reopen duplication output")]
    AccessLost,
    /// The D3D11 device was removed (driver reset, hot-unplug). Reconstruct
    /// the entire `DxgiCapture` with `open()`.
    #[error("D3D11 device removed")]
    DeviceRemoved,
    /// An unexpected HRESULT was returned.
    #[error("DXGI HRESULT {0:#010x}")]
    Hresult(i32),
}

impl From<CaptureError> for AgentError {
    fn from(e: CaptureError) -> Self {
        AgentError::CollectorRuntime { name: "screen", reason: e.to_string() }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Public types
// ──────────────────────────────────────────────────────────────────────────────

/// A single captured desktop frame (raw BGRA pixels).
pub struct CapturedFrame {
    /// BGRA pixel data, row-major, no row padding.
    pub pixels: Vec<u8>,
    /// Frame width in pixels.
    pub width: u32,
    /// Frame height in pixels.
    pub height: u32,
    /// Monitor index (Phase 1: always 0).
    pub monitor_index: u32,
}

/// DXGI-based desktop capture session for a single monitor.
///
/// # Thread safety
///
/// `IDXGIOutputDuplication` is **not** thread-safe. The session must be used
/// from the thread that called `open`. Wrap in a dedicated `std::thread` or a
/// `tokio::task::spawn_blocking` closure — never share across async tasks.
///
/// # Drop behaviour
///
/// Dropping `DxgiCapture` releases all COM references via the `windows-rs`
/// `Interface` smart pointer wrappers. No explicit cleanup is needed.
pub struct DxgiCapture {
    device:      ID3D11Device,
    context:     ID3D11DeviceContext,
    duplication: IDXGIOutputDuplication,
    width:       u32,
    height:      u32,
    /// JPEG quality used by `grab_frame` (1–100).
    quality:     u8,
}

// ──────────────────────────────────────────────────────────────────────────────
// impl DxgiCapture
// ──────────────────────────────────────────────────────────────────────────────

impl DxgiCapture {
    /// Opens a Desktop Duplication session for `monitor_index`.
    ///
    /// Phase 1: only `monitor_index = 0` is validated. Non-zero indices work
    /// if the adapter has the corresponding output, but are untested.
    ///
    /// `quality` sets the JPEG compression quality for `grab_frame` (75 is a
    /// reasonable default matching most UAM products).
    ///
    /// # Errors
    ///
    /// Returns `AgentError::CollectorStart` if D3D11 device creation, adapter
    /// enumeration, or `DuplicateOutput` fail.
    pub fn open(monitor_index: u32, quality: u8) -> Result<Self> {
        // SAFETY: We initialise all out-pointers, check return codes, and
        // wrap every raw COM pointer immediately in windows-rs smart pointers
        // which manage AddRef/Release. No raw pointer escapes this block.
        unsafe { Self::open_impl(monitor_index, quality) }
            .map_err(|e| AgentError::CollectorStart { name: "screen", reason: e.to_string() })
    }

    /// Re-opens the output duplication session after `AccessLost`.
    ///
    /// Reuses the existing D3D11 device. If re-open also fails with
    /// `DeviceRemoved`, discard `self` and call `open()` again.
    ///
    /// # Errors
    ///
    /// Returns `AgentError::CollectorRuntime` if `DuplicateOutput` fails.
    pub fn reopen(&mut self) -> Result<()> {
        // SAFETY: same contract as open_impl.
        unsafe { self.reopen_impl() }
            .map_err(|e| AgentError::CollectorRuntime { name: "screen", reason: e.to_string() })
    }

    /// Captures one frame and returns the raw BGRA pixels.
    ///
    /// Blocks for up to 100 ms waiting for a desktop update.
    ///
    /// # Errors
    ///
    /// - `FrameTimeout`   — no frame within 100 ms; retry on the next tick.
    /// - `AccessLost`     — call `reopen()` then retry.
    /// - `DeviceRemoved`  — create a new `DxgiCapture` with `open()`.
    pub fn capture_frame(&self) -> Result<CapturedFrame> {
        // SAFETY: same contract as open_impl.
        unsafe { self.capture_impl() }
    }

    /// Grabs one frame and returns JPEG-encoded bytes.
    ///
    /// Internally calls `capture_frame` then `encode_jpeg`.
    ///
    /// # Errors
    ///
    /// Propagates `capture_frame` errors. JPEG encoding failure is mapped to
    /// `AgentError::Internal` (should never occur with an in-memory writer).
    pub fn grab_frame(&self) -> Result<Vec<u8>> {
        let frame = self.capture_frame()?;
        Self::encode_jpeg(&frame.pixels, frame.width, frame.height, self.quality)
    }

    /// JPEG-encodes a BGRA frame.
    ///
    /// Converts BGRA → RGB24 in a single pass (DXGI fills alpha with 0xFF for
    /// desktop surfaces; dropping it is safe).
    ///
    /// # Errors
    ///
    /// Returns `AgentError::Internal` on encoder I/O failure.
    pub fn encode_jpeg(bgra: &[u8], width: u32, height: u32, quality: u8) -> Result<Vec<u8>> {
        // BGRA → RGB: swap B↔R, discard A.
        let pixel_count = (width * height) as usize;
        let mut rgb = Vec::with_capacity(pixel_count * 3);
        for chunk in bgra.chunks_exact(4) {
            rgb.push(chunk[2]); // R (was B at index 0 in BGRA, R at index 2)
            rgb.push(chunk[1]); // G
            rgb.push(chunk[0]); // B (was R at index 2 in BGRA, B at index 0)
        }

        let mut buf: Vec<u8> = Vec::with_capacity(rgb.len() / 4);
        JpegEncoder::new_with_quality(&mut buf, quality)
            .encode(&rgb, width, height, ColorType::Rgb8)
            .map_err(|e| AgentError::Internal(format!("JPEG encode: {e}")))?;
        Ok(buf)
    }

    // ── unsafe implementation helpers ─────────────────────────────────────────

    unsafe fn open_impl(monitor_index: u32, quality: u8) -> std::result::Result<Self, CaptureError> {
        // 1. Create D3D11 hardware device.
        let mut device_opt: Option<ID3D11Device> = None;
        let mut context_opt: Option<ID3D11DeviceContext> = None;
        D3D11CreateDevice(
            None,
            D3D_DRIVER_TYPE_HARDWARE,
            None,
            Default::default(),
            None,
            D3D11_SDK_VERSION,
            Some(&mut device_opt),
            None,
            Some(&mut context_opt),
        )
        .map_err(|e| CaptureError::Hresult(e.code().0))?;

        let device  = device_opt.ok_or(CaptureError::Hresult(0x8000_FFFF_u32 as i32))?;
        let context = context_opt.ok_or(CaptureError::Hresult(0x8000_FFFF_u32 as i32))?;

        // 2. DXGI adapter from device.
        let dxgi_device: IDXGIDevice = device.cast()
            .map_err(|e| CaptureError::Hresult(e.code().0))?;
        let adapter = dxgi_device.GetAdapter()
            .map_err(|e| CaptureError::Hresult(e.code().0))?;

        // 3. Enumerate outputs.
        // TODO (Phase 2): loop over all outputs for multi-monitor support.
        let output: IDXGIOutput = adapter.EnumOutputs(monitor_index)
            .map_err(|e| CaptureError::Hresult(e.code().0))?;

        // 4. Get output dimensions.
        let mut desc = DXGI_OUTPUT_DESC::default();
        output.GetDesc(&mut desc)
            .map_err(|e| CaptureError::Hresult(e.code().0))?;
        let width  = (desc.DesktopCoordinates.right  - desc.DesktopCoordinates.left) as u32;
        let height = (desc.DesktopCoordinates.bottom - desc.DesktopCoordinates.top) as u32;

        // 5. IDXGIOutput1 for DuplicateOutput.
        let output1: IDXGIOutput1 = output.cast()
            .map_err(|e| CaptureError::Hresult(e.code().0))?;

        // 6. Open duplication session.
        let duplication = output1.DuplicateOutput(&device)
            .map_err(|e| {
                let hr = e.code().0;
                if hr == DXGI_ERROR_ACCESS_LOST {
                    CaptureError::AccessLost
                } else {
                    CaptureError::Hresult(hr)
                }
            })?;

        Ok(Self { device, context, duplication, width, height, quality })
    }

    unsafe fn reopen_impl(&mut self) -> std::result::Result<(), CaptureError> {
        let dxgi_device: IDXGIDevice = self.device.cast()
            .map_err(|e| CaptureError::Hresult(e.code().0))?;
        let adapter = dxgi_device.GetAdapter()
            .map_err(|e| CaptureError::Hresult(e.code().0))?;
        let output: IDXGIOutput = adapter.EnumOutputs(0)
            .map_err(|e| CaptureError::Hresult(e.code().0))?;
        let output1: IDXGIOutput1 = output.cast()
            .map_err(|e| CaptureError::Hresult(e.code().0))?;
        self.duplication = output1.DuplicateOutput(&self.device)
            .map_err(|e| {
                let hr = e.code().0;
                if hr == DXGI_ERROR_DEVICE_REMOVED {
                    CaptureError::DeviceRemoved
                } else {
                    CaptureError::Hresult(hr)
                }
            })?;
        Ok(())
    }

    unsafe fn capture_impl(&self) -> Result<CapturedFrame> {
        let mut frame_info = DXGI_OUTDUPL_FRAME_INFO::default();
        let mut resource_opt = None;

        // AcquireNextFrame: 100 ms timeout.
        self.duplication
            .AcquireNextFrame(100, &mut frame_info, &mut resource_opt)
            .map_err(|e| {
                let hr = e.code().0;
                AgentError::from(match hr {
                    DXGI_ERROR_WAIT_TIMEOUT   => CaptureError::FrameTimeout,
                    DXGI_ERROR_ACCESS_LOST    => CaptureError::AccessLost,
                    DXGI_ERROR_DEVICE_REMOVED => CaptureError::DeviceRemoved,
                    other                     => CaptureError::Hresult(other),
                })
            })?;

        // Copy GPU frame and read back pixels, then always release.
        let result = self.copy_to_staging_and_read(&resource_opt);
        let _ = self.duplication.ReleaseFrame();
        result
    }

    /// Copies the GPU desktop texture to a CPU-readable staging texture and
    /// reads back the BGRA row data. Must only be called between
    /// `AcquireNextFrame` and `ReleaseFrame`.
    unsafe fn copy_to_staging_and_read(
        &self,
        resource_opt: &Option<windows::Win32::Graphics::Dxgi::IDXGIResource>,
    ) -> Result<CapturedFrame> {
        // QI desktop resource → ID3D11Texture2D.
        let desktop_res = resource_opt
            .as_ref()
            .ok_or_else(|| AgentError::Internal("AcquireNextFrame resource is None".into()))?;
        let gpu_tex: ID3D11Texture2D = desktop_res.cast().map_err(|e| {
            AgentError::CollectorRuntime {
                name: "screen",
                reason: format!("QI IDXGIResource → ID3D11Texture2D: {e}"),
            }
        })?;

        // Allocate CPU-readable staging texture.
        let staging_desc = D3D11_TEXTURE2D_DESC {
            Width:          self.width,
            Height:         self.height,
            MipLevels:      1,
            ArraySize:      1,
            Format:         DXGI_FORMAT_B8G8R8A8_UNORM,
            SampleDesc:     DXGI_SAMPLE_DESC { Count: 1, Quality: 0 },
            Usage:          D3D11_USAGE_STAGING,
            BindFlags:      D3D11_BIND_FLAG(0),
            CPUAccessFlags: D3D11_CPU_ACCESS_READ,
            MiscFlags:      Default::default(),
        };
        let mut staging_opt: Option<ID3D11Texture2D> = None;
        self.device
            .CreateTexture2D(&staging_desc, None, Some(&mut staging_opt))
            .map_err(|e| AgentError::CollectorRuntime {
                name: "screen",
                reason: format!("CreateTexture2D (staging): {e}"),
            })?;
        let staging = staging_opt
            .ok_or_else(|| AgentError::Internal("staging texture is None after create".into()))?;

        // GPU → staging copy.
        self.context.CopyResource(&staging, &gpu_tex);

        // Map for CPU read.
        // windows 0.54: Map() returns Result<()> and takes the mapped subresource
        // as an Option<*mut D3D11_MAPPED_SUBRESOURCE> out-pointer. We pass Some(&mut mapped)
        // and then read the filled struct after the call succeeds.
        let mut mapped = D3D11_MAPPED_SUBRESOURCE::default();
        self.context
            .Map(&staging, 0, D3D11_MAP_READ, 0, Some(&mut mapped))
            .map_err(|e| AgentError::CollectorRuntime {
                name: "screen",
                reason: format!("Map staging texture: {e}"),
            })?;

        // Read rows. `RowPitch` may include padding bytes; we copy only the
        // pixel data portion (width * 4 bytes per row).
        let row_pitch    = mapped.RowPitch as usize;
        let pixel_stride = self.width as usize * 4;
        let mut pixels   = Vec::with_capacity(self.width as usize * self.height as usize * 4);
        let src = mapped.pData as *const u8;
        for row in 0..self.height as usize {
            // SAFETY: `src` is a valid pointer to mapped GPU memory for the
            // duration of the Map call. We access only within bounds defined
            // by the descriptor we created above.
            let row_slice = std::slice::from_raw_parts(src.add(row * row_pitch), pixel_stride);
            pixels.extend_from_slice(row_slice);
        }

        self.context.Unmap(&staging, 0);

        Ok(CapturedFrame {
            pixels,
            width:         self.width,
            height:        self.height,
            monitor_index: 0,
        })
    }
}
