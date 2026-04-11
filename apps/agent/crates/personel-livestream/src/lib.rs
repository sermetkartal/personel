//! `personel-livestream` — live view producer interface.
//!
//! Handles `LiveViewStart` / `LiveViewStop` server messages and manages the
//! DXGI → encode → LiveKit publish pipeline.
//!
//! # Phase 1 status
//!
//! The LiveKit Rust SDK (`livekit` crate) is in active development and its
//! API surface is not yet stable enough to depend on without risk of
//! interface churn. This crate provides the control interface only; the
//! capture pipeline (`DxgiCapture` → WebRTC track) is a Phase 1 deliverable
//! pending SDK maturity assessment.
//!
//! # Design
//!
//! - `LiveStreamController::handle_start` verifies the `control_signature`
//!   from the `LiveViewStart` proto message against the policy-signing key.
//! - A live session token (`agent_token`) is passed to the LiveKit SDK which
//!   handles WebRTC negotiation and publish.
//! - All session boundaries emit `live_view.started` / `live_view.stopped`
//!   events with hash-chained audit entries.

#![deny(unsafe_code)]
#![deny(missing_docs)]
#![warn(clippy::pedantic)]

use std::sync::Arc;

use tokio::sync::Mutex;
use tracing::{info, warn};

use personel_core::error::{AgentError, Result};
use personel_proto::v1::{LiveViewStart, LiveViewStop};

/// State of the live stream session.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum LiveStreamState {
    /// No active session.
    Idle,
    /// Session is active.
    Active {
        /// LiveKit room name.
        room: String,
    },
}

/// Controls the live view capture pipeline.
pub struct LiveStreamController {
    state: Arc<Mutex<LiveStreamState>>,
}

impl LiveStreamController {
    /// Creates a new controller in the idle state.
    #[must_use]
    pub fn new() -> Self {
        Self { state: Arc::new(Mutex::new(LiveStreamState::Idle)) }
    }

    /// Handles a `LiveViewStart` server message.
    ///
    /// Verifies the control signature, starts the DXGI capture pipeline,
    /// and publishes to the LiveKit room.
    ///
    /// # Errors
    ///
    /// Returns [`AgentError::Internal`] until the pipeline is implemented.
    pub async fn handle_start(&self, msg: LiveViewStart) -> Result<()> {
        // TODO: verify msg.control_signature against policy signing key.
        // TODO: check msg.not_after > now.
        // TODO: start DxgiCapture and connect to LiveKit via livekit-rust SDK.

        let room = msg.livekit_room.clone();
        info!(%room, "live view start requested (stub — not yet wired)");

        let mut state = self.state.lock().await;
        if *state != LiveStreamState::Idle {
            warn!("live view start requested while already active");
            return Err(AgentError::Internal("live view already active".into()));
        }
        *state = LiveStreamState::Active { room };

        // TODO: emit live_view.started event.
        Ok(())
    }

    /// Handles a `LiveViewStop` server message.
    ///
    /// # Errors
    ///
    /// Returns an error if no session is active.
    pub async fn handle_stop(&self, msg: LiveViewStop) -> Result<()> {
        let mut state = self.state.lock().await;
        if *state == LiveStreamState::Idle {
            warn!("live view stop requested while idle");
            return Err(AgentError::Internal("live view not active".into()));
        }
        info!(reason = %msg.reason, "live view stop");
        *state = LiveStreamState::Idle;
        // TODO: stop DXGI pipeline, disconnect from LiveKit.
        // TODO: emit live_view.stopped event.
        Ok(())
    }

    /// Returns the current state.
    pub async fn state(&self) -> LiveStreamState {
        self.state.lock().await.clone()
    }
}

impl Default for LiveStreamController {
    fn default() -> Self {
        Self::new()
    }
}
