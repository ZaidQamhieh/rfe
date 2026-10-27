//! The health and metrics screen: what the agent says about itself (`GET /health`, `GET /status`)
//! and the live host figures an administrator may read (`GET /metrics`).
//!
//! Three read-only requests, all through the pinned client, each with its own short timeout and
//! sent at the same time, so an agent that is down or silent costs at most [`REQUEST_TIMEOUT`] and
//! a half-working agent still shows what it can. Only `/health` is required: a failure there is the
//! screen's error. `/status` and `/metrics` degrade into a note, and a non-admin session (the agent
//! answers `/metrics` with 403) is a state of its own, not an error. Nothing is kept between calls:
//! the agent keeps no history and neither does the app.

use crate::agent_client::{normalize_fingerprint, AgentClient, AgentError};
use crate::applog;
use crate::flows;
use crate::secrets::Offloaded;
use serde::{Deserialize, Serialize};
use std::path::Path;
use std::time::Duration;

/// How long each of the three requests may take before the screen reports the agent as silent.
pub const REQUEST_TIMEOUT: Duration = Duration::from_secs(5);

/// `GET /health`. Only `status` is guaranteed; the rest is sent to a caller with a valid token, and
/// every field is optional here so an older or reduced answer still reads.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", default)]
pub struct Health {
    pub status: String,
    pub name: String,
    pub version: String,
    pub os: String,
    pub read_only: Option<bool>,
    /// The LAN `host:port` the agent says it can be reached at.
    pub address: String,
    pub tailscale_address: String,
    pub mac_address: String,
    /// Protocol revision; an agent that sends none is revision 1 (see `protocol/compat.md`).
    #[serde(deserialize_with = "lenient_api_version", default = "one")]
    pub api_version: i64,
    /// Optional capabilities the agent has. Absent, null or malformed means none.
    #[serde(deserialize_with = "lenient_features")]
    pub features: Vec<String>,
}

fn one() -> i64 {
    1
}

fn lenient_api_version<'de, D: serde::Deserializer<'de>>(d: D) -> Result<i64, D::Error> {
    let v = serde_json::Value::deserialize(d)?;
    Ok(v.as_i64().filter(|n| *n >= 1).unwrap_or(1))
}

fn lenient_features<'de, D: serde::Deserializer<'de>>(d: D) -> Result<Vec<String>, D::Error> {
    let v = serde_json::Value::deserialize(d)?;
    Ok(v.as_array()
        .map(|a| {
            a.iter()
                .filter_map(|x| x.as_str().map(str::to_owned))
                .collect()
        })
        .unwrap_or_default())
}

impl Health {
    /// Whether the agent advertises `feature`. Gate newer UI on this, never on the version string.
    pub fn supports(&self, feature: &str) -> bool {
        self.features.iter().any(|f| f == feature)
    }
}

/// `GET /status`: uptime and disk space. Counts are whole numbers of seconds and bytes.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", default)]
pub struct AgentStatus {
    pub version: String,
    pub uptime_seconds: Option<i64>,
    /// `os/arch`, for example `linux/amd64`.
    pub platform: String,
    pub free_bytes: Option<i64>,
    pub total_bytes: Option<i64>,
}

/// `GET /metrics`. `rx_bytes` and `tx_bytes` are totals since the agent started (bytes, not a
/// rate); `cpu_percent` and `ram_percent` are 0 to 100 at the moment of reading; `ts_ms` is the
/// agent's clock, Unix milliseconds. A reading taken later shows the rate by subtracting.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", default)]
pub struct Metrics {
    pub rx_bytes: Option<i64>,
    pub tx_bytes: Option<i64>,
    pub cpu_percent: Option<f64>,
    pub ram_percent: Option<f64>,
    pub ts_ms: Option<i64>,
}

/// Everything the screen shows, from one refresh.
#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Snapshot {
    /// The `host:port` this computer is talking to.
    pub host: String,
    /// The certificate fingerprint pinned for that address (64 lowercase hex characters).
    pub fingerprint: String,
    pub health: Health,
    /// `None` when `/status` did not answer; `status_note` says why.
    pub status: Option<AgentStatus>,
    pub status_note: String,
    /// `None` when there are no readings; either `metrics_forbidden` or `metrics_note` says why.
    pub metrics: Option<Metrics>,
    /// The agent refused `/metrics` with 403: this login is not an administrator's.
    pub metrics_forbidden: bool,
    pub metrics_note: String,
}

/// Reads the three routes as the signed-in device `token`. Returns an error when `/health` fails
/// (nothing reachable, certificate refused, timeout) or when the agent rejects the token (401).
pub async fn snapshot(
    client: &AgentClient,
    host: &str,
    fingerprint: &str,
    token: &str,
    timeout: Duration,
) -> Result<Snapshot, AgentError> {
    let (health, status, metrics) = tokio::join!(
        client.get_fixed_json::<Health>(token, "/health", timeout),
        client.get_fixed_json::<AgentStatus>(token, "/status", timeout),
        client.get_fixed_json::<Metrics>(token, "/metrics", timeout),
    );
    // An invalid or revoked token still gets a bare `/health`; the other two routes say so.
    for r in [status.as_ref().err(), metrics.as_ref().err()]
        .into_iter()
        .flatten()
    {
        if matches!(r, AgentError::Server { status: 401, .. }) {
            return Err(AgentError::Server {
                status: 401,
                code: "UNAUTHORIZED".into(),
                message: String::new(),
            });
        }
    }
    let health = health?;
    if health.status != "ok" {
        return Err(AgentError::Local(
            "unexpected response: the agent's health answer does not say ok".into(),
        ));
    }
    let (status, status_note) = match status {
        Ok(s) => (Some(s), String::new()),
        Err(e) => (None, e.to_string()),
    };
    let (metrics, metrics_forbidden, metrics_note) = match metrics {
        Ok(m) => (Some(m), false, String::new()),
        Err(AgentError::Server { status: 403, .. }) => (None, true, String::new()),
        Err(e) => (None, false, e.to_string()),
    };
    Ok(Snapshot {
        host: host.to_string(),
        fingerprint: normalize_fingerprint(fingerprint),
        health,
        status,
        status_note,
        metrics,
        metrics_forbidden,
        metrics_note,
    })
}

/// The saved session's refresh: loads the login, uses the pinned certificate for its address and
/// reads the snapshot. A 401 drops the dead token so the window goes back to sign-in, the same as
/// for the device list.
pub async fn agent_health(dir: &Path, store: &Offloaded) -> Result<Snapshot, AgentError> {
    let r = agent_health_inner(dir, store).await;
    match &r {
        Ok(_) => applog::debug("read the agent's health and metrics"),
        Err(e) => applog::error(&format!("reading the agent's health failed: {e}")),
    }
    r
}

async fn agent_health_inner(dir: &Path, store: &Offloaded) -> Result<Snapshot, AgentError> {
    let s = {
        let dir = dir.to_path_buf();
        store
            .run(move |s| flows::load_saved(&dir, s))
            .await
            .map_err(AgentError::Local)?
    };
    if s.token.is_empty() || s.host.is_empty() {
        return Err(AgentError::Local("not signed in".into()));
    }
    let key = flows::pin_key(&s.host);
    let pinned = flows::list_pins(dir)
        .map_err(AgentError::Local)?
        .into_iter()
        .find(|p| flows::pin_key(&p.host) == key)
        .map(|p| p.fingerprint)
        .ok_or_else(|| {
            AgentError::Local(format!(
                "{} is no longer a trusted agent; connect and compare its fingerprint again",
                s.host
            ))
        })?;
    let client = AgentClient::pinned(&s.host, &pinned)?;
    let got = snapshot(&client, &s.host, &pinned, &s.token, REQUEST_TIMEOUT).await;
    if let Err(AgentError::Server { status: 401, .. }) = &got {
        let dir = dir.to_path_buf();
        let token = s.token.clone();
        store
            .run(move |st| flows::drop_refused_token(&dir, st, &token))
            .await
            .map_err(AgentError::Local)?;
    }
    got
}

#[cfg(test)]
mod compat_tests {
    use super::Health;

    #[test]
    fn an_old_agent_is_revision_one_with_no_features() {
        let h: Health = serde_json::from_str(r#"{"status":"ok","version":"1.42.5"}"#).unwrap();
        assert_eq!(h.api_version, 1);
        assert!(!h.supports("pairRequest"));
    }

    #[test]
    fn a_new_agent_lists_features_and_unknown_fields_are_ignored() {
        let h: Health = serde_json::from_str(
            r#"{"status":"ok","apiVersion":2,"features":["pairRequest","future"],"extra":{"a":1}}"#,
        )
        .unwrap();
        assert_eq!(h.api_version, 2);
        assert!(h.supports("pairRequest"));
    }

    #[test]
    fn malformed_values_do_not_fail_health() {
        let h: Health =
            serde_json::from_str(r#"{"status":"ok","apiVersion":"x","features":null}"#).unwrap();
        assert_eq!(h.api_version, 1);
        assert!(h.features.is_empty());
    }
}
