//! `personel-transport` — gRPC client with mTLS, reconnect, and backpressure.

#![deny(unsafe_code)]
#![deny(missing_docs)]
#![warn(clippy::pedantic)]
#![allow(clippy::module_name_repetitions)]

pub mod client;
pub mod envelope;
pub mod tls;
