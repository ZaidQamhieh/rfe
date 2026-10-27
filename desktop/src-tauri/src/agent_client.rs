//! HTTPS client for the RFE agent with certificate pinning.
//!
//! The agent uses a self-signed certificate, so there is no CA to trust. The
//! identity of the agent is the SHA-256 of its leaf certificate (the same hex
//! string `rfe status` prints). Two modes:
//!
//! - capture: accepts any certificate and records its fingerprint. Used only
//!   by `capture_fingerprint`, which sends one unauthenticated GET and no
//!   credentials, so the user can compare the fingerprint before trusting it.
//! - pinned: accepts only a certificate whose fingerprint matches. The only
//!   way to build an `AgentClient` (the type that can send a password or a
//!   token) is `AgentClient::pinned`, so credentials cannot go out on an
//!   unverified connection.

use crate::identity::Identity;
use hmac::{Hmac, Mac};
use rustls::client::danger::{HandshakeSignatureValid, ServerCertVerified, ServerCertVerifier};
use rustls::crypto::{verify_tls12_signature, verify_tls13_signature, CryptoProvider};
use rustls::pki_types::{CertificateDer, ServerName, UnixTime};
use rustls::{ClientConfig, DigitallySignedStruct, Error as TlsError, SignatureScheme};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::sync::{Arc, Mutex};
use std::time::Duration;

pub const CLIENT_VERSION: &str = concat!("desktop-", env!("CARGO_PKG_VERSION"));

#[derive(Debug)]
pub enum AgentError {
    /// Connection or TLS failure (including a fingerprint mismatch).
    Network(String),
    /// The agent answered with an error body `{code, message}`.
    Server {
        status: u16,
        code: String,
        message: String,
    },
    /// Bad local input or state.
    Local(String),
}

impl std::fmt::Display for AgentError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            AgentError::Network(m) if m.contains("fingerprint mismatch") => write!(
                f,
                "This agent's certificate is not the one you trusted ({m}). Do not sign in. If you \
                 replaced or reinstalled the agent, open Servers and choose Forget server on its card, \
                 then add it again and compare its fingerprint."
            ),
            AgentError::Network(m) => write!(
                f,
                "cannot reach the agent securely: {m}. Check that the agent is running and the \
                 address is right."
            ),
            AgentError::Server { code, message, .. } => match known_message(code) {
                Some(text) => write!(f, "{text}"),
                None => write!(f, "{code}: {message}"),
            },
            AgentError::Local(m) => write!(f, "{m}"),
        }
    }
}

/// Every error code the agent's sign-in and pairing endpoints return, each with its own
/// wording and what to do next. A code not listed here is shown as the agent sent it.
pub const KNOWN_CODES: &[&str] = &[
    "INVALID_CREDENTIALS",
    "INVALID_CODE",
    "INVALID_NONCE",
    "INVALID_SIGNATURE",
    "DEVICE_KEY_REQUIRED",
    "DEVICE_KEY_MISMATCH",
    "RATE_LIMITED",
    "PAIR_BUSY",
    "UNAUTHORIZED",
    "FORBIDDEN",
    "NOT_FOUND",
    "BAD_REQUEST",
    "INTERNAL",
];

/// The message for an agent error code, or `None` for a code this app does not know.
pub fn known_message(code: &str) -> Option<&'static str> {
    Some(match code {
        "INVALID_CREDENTIALS" => "Wrong username or password.",
        "INVALID_CODE" => {
            "That pairing code is wrong, expired or already used. Generate a new one on the PC."
        }
        "INVALID_NONCE" => {
            "The agent no longer accepts this sign-in attempt (it expired or was already used). \
             Try again."
        }
        "INVALID_SIGNATURE" => {
            "The agent could not verify this computer's device key. Try again; if it keeps \
             failing, report it."
        }
        "DEVICE_KEY_REQUIRED" => {
            "The agent requires a device key proof that this app did not send. This is a bug in \
             the app."
        }
        "DEVICE_KEY_MISMATCH" => {
            "The agent already knows a different key for this computer. Remove this computer from \
             the agent's device list (rfe remove <id> on the PC), then sign in again."
        }
        "RATE_LIMITED" => {
            "Too many attempts. The agent allows only a few sign-in and pairing attempts per \
             minute. Wait a minute, then try again."
        }
        "PAIR_BUSY" => {
            "The agent already has pairing requests waiting for approval on the PC. Answer them or \
             wait for them to expire, then try again."
        }
        "UNAUTHORIZED" => "The agent no longer accepts this login. Sign in again.",
        "FORBIDDEN" => {
            "This login is not allowed to do that. Sign in with the account, not a pairing code, \
             to manage devices."
        }
        "NOT_FOUND" => "The agent has no such item. It may have expired.",
        "BAD_REQUEST" => "The agent refused the request as malformed. This is a bug in the app.",
        "INTERNAL" => "The agent had an internal error. Check its log on the PC.",
        _ => return None,
    })
}

impl std::error::Error for AgentError {}

#[derive(Debug)]
enum Mode {
    Pin(String),
    Capture(Arc<Mutex<Option<String>>>),
}

#[derive(Debug)]
struct PinVerifier {
    mode: Mode,
    provider: Arc<CryptoProvider>,
}

fn fingerprint_of(der: &[u8]) -> String {
    hex::encode(Sha256::digest(der))
}

impl ServerCertVerifier for PinVerifier {
    fn verify_server_cert(
        &self,
        end_entity: &CertificateDer<'_>,
        _intermediates: &[CertificateDer<'_>],
        _server_name: &ServerName<'_>,
        _ocsp_response: &[u8],
        _now: UnixTime,
    ) -> Result<ServerCertVerified, TlsError> {
        let seen = fingerprint_of(end_entity.as_ref());
        match &self.mode {
            Mode::Pin(expected) => {
                if &seen == expected {
                    Ok(ServerCertVerified::assertion())
                } else {
                    Err(TlsError::General(format!(
                        "certificate fingerprint mismatch (pinned {expected}, agent presented {seen})"
                    )))
                }
            }
            Mode::Capture(slot) => {
                *slot.lock().unwrap() = Some(seen);
                Ok(ServerCertVerified::assertion())
            }
        }
    }

    fn verify_tls12_signature(
        &self,
        message: &[u8],
        cert: &CertificateDer<'_>,
        dss: &DigitallySignedStruct,
    ) -> Result<HandshakeSignatureValid, TlsError> {
        verify_tls12_signature(
            message,
            cert,
            dss,
            &self.provider.signature_verification_algorithms,
        )
    }

    fn verify_tls13_signature(
        &self,
        message: &[u8],
        cert: &CertificateDer<'_>,
        dss: &DigitallySignedStruct,
    ) -> Result<HandshakeSignatureValid, TlsError> {
        verify_tls13_signature(
            message,
            cert,
            dss,
            &self.provider.signature_verification_algorithms,
        )
    }

    fn supported_verify_schemes(&self) -> Vec<SignatureScheme> {
        self.provider
            .signature_verification_algorithms
            .supported_schemes()
    }
}

/// Time to reach the agent. A PC that is off or on another network fails here, quickly.
const CONNECT_TIMEOUT: Duration = Duration::from_secs(10);
/// Whole-request limit for ordinary calls. Above the agent's own 15 s search and recents budgets, so
/// the agent's partial "ran out of time" answer arrives instead of being cut off.
const REQUEST_TIMEOUT: Duration = Duration::from_secs(30);
/// Whole-request limit for calls that read or rewrite a whole tree (copy, move, archive, checksum,
/// delete, trash). The agent runs them to completion inside the request.
const LONG_OP_TIMEOUT: Duration = Duration::from_secs(300);

/// The whole-request limit for a signed-in file-operation call.
fn route_timeout(method: &reqwest::Method, route: &str) -> Duration {
    let long = matches!(
        route,
        "/fs/copy"
            | "/fs/move"
            | "/fs/compress"
            | "/fs/extract"
            | "/fs/archive"
            | "/fs/checksum"
            | "/fs/checksums"
            | "/trash"
            | "/trash/restore"
    ) || (route == "/fs" && method == reqwest::Method::DELETE);
    if long {
        LONG_OP_TIMEOUT
    } else {
        REQUEST_TIMEOUT
    }
}

fn build_http(mode: Mode) -> Result<reqwest::Client, AgentError> {
    let provider = Arc::new(rustls::crypto::ring::default_provider());
    let config = ClientConfig::builder_with_provider(provider.clone())
        .with_safe_default_protocol_versions()
        .map_err(|e| AgentError::Local(format!("tls config: {e}")))?
        .dangerous()
        .with_custom_certificate_verifier(Arc::new(PinVerifier { mode, provider }))
        .with_no_client_auth();
    reqwest::Client::builder()
        .use_preconfigured_tls(config)
        // Agent traffic goes straight to the agent; never via an environment proxy.
        .no_proxy()
        .redirect(reqwest::redirect::Policy::none())
        .connect_timeout(CONNECT_TIMEOUT)
        .timeout(REQUEST_TIMEOUT)
        .build()
        .map_err(|e| AgentError::Local(format!("http client: {e}")))
}

/// Lowercase hex with separators and whitespace removed, so `AB:CD` and `abcd` compare equal.
pub fn normalize_fingerprint(s: &str) -> String {
    s.chars()
        .filter(|c| c.is_ascii_hexdigit())
        .collect::<String>()
        .to_ascii_lowercase()
}

/// The code both sides show so the user can tell the agent they reached is the one that holds the
/// certificate this app saw: `HMAC-SHA256(key = certificate SHA-256, msg = "rfe-pair-sas\n" +
/// clientNonce + requestId)`, first 8 bytes as a big-endian number mod 10^8, as "1234 5678". A
/// machine relaying the connection presents a different certificate and so gets a different code.
pub fn match_code(
    fingerprint_hex: &str,
    client_nonce_hex: &str,
    request_id: &str,
) -> Result<String, AgentError> {
    let fp = hex::decode(normalize_fingerprint(fingerprint_hex))
        .ok()
        .filter(|b| b.len() == 32)
        .ok_or_else(|| AgentError::Local("bad fingerprint".into()))?;
    let nonce = hex::decode(client_nonce_hex)
        .ok()
        .filter(|b| b.len() == 16)
        .ok_or_else(|| AgentError::Local("bad client nonce".into()))?;
    let mut mac = Hmac::<Sha256>::new_from_slice(&fp).expect("HMAC takes any key length");
    mac.update(b"rfe-pair-sas\n");
    mac.update(&nonce);
    mac.update(request_id.as_bytes());
    let digest = mac.finalize().into_bytes();
    let n = u64::from_be_bytes(digest[..8].try_into().expect("8 bytes")) % 100_000_000;
    Ok(format!("{:04} {:04}", n / 10_000, n % 10_000))
}

/// Hex SHA-256 of the client nonce's bytes: what the agent holds until the nonce is revealed.
pub fn nonce_commitment(client_nonce_hex: &str) -> Result<String, AgentError> {
    let raw = hex::decode(client_nonce_hex)
        .map_err(|_| AgentError::Local("the client nonce is not hex".into()))?;
    Ok(hex::encode(Sha256::digest(raw)))
}

/// 16 random bytes, hex: the secret that authorises polling for one pairing request.
pub fn new_client_nonce() -> Result<String, AgentError> {
    let mut b = [0u8; 16];
    getrandom::getrandom(&mut b).map_err(|e| AgentError::Local(format!("random: {e}")))?;
    Ok(hex::encode(b))
}

/// `host:port` only: no scheme, path, userinfo or query, so a typo cannot
/// redirect credentials to another origin.
pub fn validate_hostport(hostport: &str) -> Result<(), AgentError> {
    let bad = || {
        AgentError::Local(
            "agent address must be host:port (put an IPv6 address in brackets, like [::1]:8765)"
                .into(),
        )
    };
    let ok_chars = hostport
        .chars()
        .all(|c| c.is_ascii_alphanumeric() || matches!(c, '.' | '-' | ':' | '[' | ']'));
    let (host, port) = hostport.rsplit_once(':').ok_or_else(bad)?;
    let port_ok = port.parse::<u16>().map(|p| p != 0).unwrap_or(false);
    // A host with ':' is an IPv6 literal and must be bracketed; otherwise no brackets at all.
    let host_ok = if host.contains(':') {
        host.len() > 2
            && host.starts_with('[')
            && host.ends_with(']')
            && !host[1..host.len() - 1].contains(['[', ']'])
    } else {
        !host.is_empty() && !host.contains(['[', ']'])
    };
    if !ok_chars || !port_ok || !host_ok {
        return Err(bad());
    }
    Ok(())
}

fn net(e: reqwest::Error) -> AgentError {
    // The request URL can carry a secret (the approval poll's nonce is a query parameter).
    let e = e.without_url();
    let mut msg = e.to_string();
    let mut src = std::error::Error::source(&e);
    while let Some(s) = src {
        msg = format!("{msg}: {s}");
        src = s.source();
    }
    AgentError::Network(msg)
}

/// Connects without trusting anything and returns the agent's certificate
/// fingerprint. Sends one unauthenticated GET and no credentials.
pub async fn capture_fingerprint(hostport: &str) -> Result<String, AgentError> {
    validate_hostport(hostport)?;
    let seen = Arc::new(Mutex::new(None));
    let http = build_http(Mode::Capture(seen.clone()))?;
    http.get(format!("https://{hostport}/v1/health"))
        .send()
        .await
        .map_err(net)?;
    let fp = seen.lock().unwrap().clone();
    fp.ok_or_else(|| AgentError::Local("the agent presented no certificate".into()))
}

#[derive(Deserialize)]
struct ApiError {
    code: String,
    message: String,
}

#[derive(Deserialize)]
struct Challenge {
    nonce: String,
    /// `"v2"` on agents that accept the certificate-bound proof.
    #[serde(default)]
    proof: String,
}

/// Said when an agent offers only the bare-nonce proof, before any credential is sent.
pub const ERR_AGENT_NO_BOUND_PROOF: &str =
    "This agent is too old to sign in from this app: it cannot tie the sign-in proof to its own \
     certificate. Update the agent on the PC, then try again.";

/// The app signs only a proof bound to the agent's certificate, so an agent that does not say it
/// verifies one is refused (an agent could otherwise hide its support to get a bare-nonce signature).
fn require_bound_proof(c: &Challenge) -> Result<(), AgentError> {
    if c.proof == "v2" {
        Ok(())
    } else {
        Err(AgentError::Local(ERR_AGENT_NO_BOUND_PROOF.into()))
    }
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct LoginBody<'a> {
    username: &'a str,
    password: &'a str,
    device_label: &'a str,
    device_id: &'a str,
    device_public_key: String,
    nonce: String,
    signature: String,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct PairBody<'a> {
    pairing_code: &'a str,
    device_label: &'a str,
    device_id: &'a str,
    device_public_key: String,
    nonce: String,
    signature: String,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct PairRequestBody<'a> {
    device_label: &'a str,
    device_id: &'a str,
    device_public_key: String,
    nonce: String,
    signature: String,
    client_nonce_commit: String,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct RevealBody<'a> {
    client_nonce: &'a str,
}

/// A pairing request the agent is holding for the owner to answer.
#[derive(Debug, Clone, Default, Deserialize)]
#[serde(rename_all = "camelCase", default)]
pub struct PairRequestStarted {
    pub request_id: String,
    pub expires_in_seconds: u64,
}

/// Where a pairing request stands. `Approved` carries the token and is returned once only.
#[derive(Debug)]
pub enum PairPoll {
    Pending,
    Rejected,
    Approved(LoginOk),
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase", default)]
#[derive(Default)]
struct PollBody {
    status: String,
    device_token: String,
    device_id: String,
    agent_name: String,
    cert_fingerprint: String,
}

#[derive(Default, Deserialize)]
#[serde(rename_all = "camelCase", default)]
pub struct LoginOk {
    pub device_token: String,
    pub device_id: String,
    pub agent_name: String,
    pub cert_fingerprint: String,
}

// Not derived: a `{:?}` in a log line or a test failure must not print the device token.
impl std::fmt::Debug for LoginOk {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("LoginOk")
            .field("device_token", &"<redacted>")
            .field("device_id", &self.device_id)
            .field("agent_name", &self.agent_name)
            .field("cert_fingerprint", &self.cert_fingerprint)
            .finish()
    }
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", default)]
pub struct Device {
    pub id: String,
    pub label: String,
    pub created: i64,
    pub last_seen: i64,
    pub revoked: bool,
    pub current: bool,
    pub last_address: String,
    pub last_version: String,
    pub via_login: bool,
}

pub struct AgentClient {
    http: reqwest::Client,
    base: String,
    fingerprint: String,
}

/// The most a JSON answer may be (a big folder listing is a few MiB) and the most an error body
/// may be. A pinned agent is the user's own, but a misbehaving one must not fill this computer's
/// memory by never ending a response.
pub(crate) const MAX_JSON_BODY: usize = 64 << 20;
const MAX_ERROR_BODY: usize = 1 << 20;

/// The body of `resp`, refused once it is longer than `max` bytes.
pub(crate) async fn read_capped(
    mut resp: reqwest::Response,
    max: usize,
) -> Result<Vec<u8>, AgentError> {
    let mut out = Vec::new();
    while let Some(chunk) = resp.chunk().await.map_err(net)? {
        if out.len() + chunk.len() > max {
            return Err(AgentError::Local(
                "unexpected response: the agent sent more data than this app accepts".into(),
            ));
        }
        out.extend_from_slice(&chunk);
    }
    Ok(out)
}

/// A JSON answer, read with a size limit.
pub(crate) async fn read_json<T: serde::de::DeserializeOwned>(
    resp: reqwest::Response,
    max: usize,
) -> Result<T, AgentError> {
    let bytes = read_capped(resp, max).await?;
    serde_json::from_slice(&bytes)
        .map_err(|e| AgentError::Local(format!("unexpected response: {e}")))
}

async fn parse<T: serde::de::DeserializeOwned>(resp: reqwest::Response) -> Result<T, AgentError> {
    let status = resp.status();
    if status.is_success() {
        return read_json(resp, MAX_JSON_BODY).await;
    }
    Err(error_of(resp).await)
}

/// The error for a non-success response: the agent's `{code, message}` body when it has one.
async fn error_of(resp: reqwest::Response) -> AgentError {
    let code = resp.status().as_u16();
    let body = read_capped(resp, MAX_ERROR_BODY).await.unwrap_or_default();
    match serde_json::from_slice::<ApiError>(&body) {
        Ok(e) => AgentError::Server {
            status: code,
            code: e.code,
            message: e.message,
        },
        Err(_) => AgentError::Server {
            status: code,
            code: format!("HTTP_{code}"),
            message: "the agent returned an unexpected error".into(),
        },
    }
}

impl AgentClient {
    /// The only constructor. `fingerprint` must be the 64-hex SHA-256 the user trusted.
    pub fn pinned(hostport: &str, fingerprint: &str) -> Result<Self, AgentError> {
        validate_hostport(hostport)?;
        let fp = normalize_fingerprint(fingerprint);
        if fp.len() != 64 {
            return Err(AgentError::Local(
                "fingerprint must be 64 hex characters".into(),
            ));
        }
        Ok(Self {
            http: build_http(Mode::Pin(fp.clone()))?,
            base: format!("https://{hostport}/v1"),
            fingerprint: fp,
        })
    }

    async fn challenge(&self) -> Result<String, AgentError> {
        let c: Challenge = parse(
            self.http
                .post(format!("{}/auth/challenge", self.base))
                .send()
                .await
                .map_err(net)?,
        )
        .await?;
        require_bound_proof(&c)?;
        Ok(c.nonce)
    }

    /// The TLS pin already guarantees the certificate; the agent's own statement of its
    /// fingerprint is a second check that fails loudly if the two ever differ.
    fn check_fingerprint(&self, ok: &LoginOk) -> Result<(), AgentError> {
        if !ok.cert_fingerprint.is_empty()
            && normalize_fingerprint(&ok.cert_fingerprint) != self.fingerprint
        {
            return Err(AgentError::Local(
                "the agent reports a different fingerprint than the pinned one".into(),
            ));
        }
        Ok(())
    }

    pub async fn login(
        &self,
        id: &Identity,
        username: &str,
        password: &str,
        label: &str,
    ) -> Result<LoginOk, AgentError> {
        let nonce = self.challenge().await?;
        let body = LoginBody {
            username,
            password,
            device_label: label,
            device_id: id.device_id(),
            device_public_key: id.public_key_b64(),
            signature: id.proof_b64(&self.fingerprint, &nonce),
            nonce,
        };
        let ok: LoginOk = parse(
            self.http
                .post(format!("{}/login", self.base))
                .json(&body)
                .send()
                .await
                .map_err(net)?,
        )
        .await?;
        self.check_fingerprint(&ok)?;
        Ok(ok)
    }

    /// Enrolls with a one-time pairing code (`POST /pair`). The agent checks the device proof
    /// before it spends the code, so a failed attempt does not burn a valid one. The token this
    /// returns belongs to an ordinary device, not an admin one.
    pub async fn pair(
        &self,
        id: &Identity,
        code: &str,
        label: &str,
    ) -> Result<LoginOk, AgentError> {
        let nonce = self.challenge().await?;
        let body = PairBody {
            pairing_code: code,
            device_label: label,
            device_id: id.device_id(),
            device_public_key: id.public_key_b64(),
            signature: id.proof_b64(&self.fingerprint, &nonce),
            nonce,
        };
        let ok: LoginOk = parse(
            self.http
                .post(format!("{}/pair", self.base))
                .json(&body)
                .send()
                .await
                .map_err(net)?,
        )
        .await?;
        self.check_fingerprint(&ok)?;
        Ok(ok)
    }

    /// Asks the owner to approve this computer at the PC (`POST /pair/request`). The device proof
    /// goes with it, and only a hash of `client_nonce`: the agent picks the request id before it
    /// sees the nonce (see `reveal_pair_request`), so a relay cannot grind an id that makes the
    /// two match codes agree.
    pub async fn pair_request(
        &self,
        id: &Identity,
        label: &str,
        client_nonce: &str,
    ) -> Result<PairRequestStarted, AgentError> {
        let nonce = self.challenge().await?;
        let body = PairRequestBody {
            device_label: label,
            device_id: id.device_id(),
            device_public_key: id.public_key_b64(),
            signature: id.proof_b64(&self.fingerprint, &nonce),
            nonce,
            client_nonce_commit: nonce_commitment(client_nonce)?,
        };
        parse(
            self.http
                .post(format!("{}/pair/request", self.base))
                .json(&body)
                .send()
                .await
                .map_err(net)?,
        )
        .await
    }

    /// Reveals the nonce committed in `pair_request` (`POST /pair/request/{id}/reveal`). Only now
    /// does the agent ask the owner; `client_nonce` is also the secret needed to poll.
    pub async fn reveal_pair_request(
        &self,
        request_id: &str,
        client_nonce: &str,
    ) -> Result<(), AgentError> {
        if !valid_pair_request_id(request_id) {
            return Err(AgentError::Local("invalid pairing request id".into()));
        }
        let resp = self
            .http
            .post(format!("{}/pair/request/{}/reveal", self.base, request_id))
            .json(&RevealBody { client_nonce })
            .send()
            .await
            .map_err(net)?;
        parse::<serde_json::Value>(resp).await.map(|_| ())
    }

    /// Asks how a pairing request stands (`GET /pair/request/{id}`). The approval, with the
    /// token, is handed out once: a second poll gets `NOT_FOUND`.
    pub async fn poll_pair_request(
        &self,
        request_id: &str,
        client_nonce: &str,
    ) -> Result<PairPoll, AgentError> {
        if request_id.is_empty() || !request_id.chars().all(|c| c.is_ascii_alphanumeric()) {
            return Err(AgentError::Local(format!(
                "unexpected request id {request_id:?}"
            )));
        }
        let body: PollBody = parse(
            self.http
                .get(format!("{}/pair/request/{request_id}", self.base))
                .query(&[("nonce", client_nonce)])
                .send()
                .await
                .map_err(net)?,
        )
        .await?;
        match body.status.as_str() {
            "pending" => Ok(PairPoll::Pending),
            "rejected" => Ok(PairPoll::Rejected),
            "approved" => {
                let ok = LoginOk {
                    device_token: body.device_token,
                    device_id: body.device_id,
                    agent_name: body.agent_name,
                    cert_fingerprint: body.cert_fingerprint,
                };
                self.check_fingerprint(&ok)?;
                Ok(PairPoll::Approved(ok))
            }
            other => Err(AgentError::Local(format!(
                "the agent answered with an unknown pairing status {other:?}"
            ))),
        }
    }

    /// Revokes this computer's own device on the agent (`DELETE /devices/{id}`), after which the
    /// agent rejects `token`. Any device may do this to itself, admin or not.
    pub async fn revoke_own_device(&self, token: &str, device_id: &str) -> Result<(), AgentError> {
        if device_id.is_empty()
            || !device_id
                .chars()
                .all(|c| c.is_ascii_alphanumeric() || matches!(c, '-' | '_' | '.'))
        {
            return Err(AgentError::Local(format!(
                "unexpected device id {device_id:?}"
            )));
        }
        let resp = self
            .http
            .delete(format!("{}/devices/{device_id}", self.base))
            .bearer_auth(token)
            .header("X-RFE-Client-Version", CLIENT_VERSION)
            .send()
            .await
            .map_err(net)?;
        if resp.status().is_success() {
            Ok(())
        } else {
            Err(error_of(resp).await)
        }
    }

    pub async fn devices(&self, token: &str) -> Result<Vec<Device>, AgentError> {
        parse(
            self.http
                .get(format!("{}/devices", self.base))
                .bearer_auth(token)
                .header("X-RFE-Client-Version", CLIENT_VERSION)
                .send()
                .await
                .map_err(net)?,
        )
        .await
    }

    // ---- feature:audit-logs ----
    /// GET one fixed agent route (`path` is a literal chosen in this crate, never user input) as
    /// `token` and decodes the JSON body. `query` values are URL-encoded by the client.
    pub(crate) async fn get_json_at<T: serde::de::DeserializeOwned>(
        &self,
        token: &str,
        path: &'static str,
        query: &[(&str, String)],
    ) -> Result<T, AgentError> {
        debug_assert!(path.starts_with('/') && !path.contains(['?', '#']));
        parse(
            self.http
                .get(format!("{}{path}", self.base))
                .query(query)
                .bearer_auth(token)
                .header("X-RFE-Client-Version", CLIENT_VERSION)
                .send()
                .await
                .map_err(net)?,
        )
        .await
    }
}

// ---- feature:file-browser ----
impl AgentClient {
    /// One signed-in JSON call on a fixed `/v1` route, for the file browser (`crate::files`). The
    /// route is a literal chosen by the caller, never user input; the user's values travel in
    /// `query` (percent-encoded by the HTTP library) or in the JSON `body`. Like every call of
    /// this client it goes over the pinned connection.
    pub(crate) async fn authed_json<T: serde::de::DeserializeOwned>(
        &self,
        token: &str,
        method: reqwest::Method,
        route: &str,
        query: &[(&str, &str)],
        body: Option<serde_json::Value>,
    ) -> Result<T, AgentError> {
        if !route.starts_with('/') || route.contains(['?', '#', '\\']) || route.contains("..") {
            return Err(AgentError::Local(format!("unexpected route {route:?}")));
        }
        let mut req = self
            .http
            .request(method.clone(), format!("{}{route}", self.base))
            .timeout(route_timeout(&method, route))
            .bearer_auth(token)
            .header("X-RFE-Client-Version", CLIENT_VERSION);
        if !query.is_empty() {
            req = req.query(query);
        }
        if let Some(body) = body {
            req = req.json(&body);
        }
        parse(req.send().await.map_err(net)?).await
    }
}
// ---- end feature:file-browser ----

// ---- feature:file-ops ----
impl AgentClient {
    fn authed_request(
        &self,
        token: &str,
        method: reqwest::Method,
        route: &str,
        query: &[(&str, &str)],
    ) -> Result<reqwest::RequestBuilder, AgentError> {
        if !route.starts_with('/') || route.contains(['?', '#', '\\']) || route.contains("..") {
            return Err(AgentError::Local(format!("unexpected route {route:?}")));
        }
        let mut req = self
            .http
            .request(method.clone(), format!("{}{route}", self.base))
            .timeout(route_timeout(&method, route))
            .bearer_auth(token)
            .header("X-RFE-Client-Version", CLIENT_VERSION);
        if !query.is_empty() {
            req = req.query(query);
        }
        Ok(req)
    }

    /// A signed-in call whose answer carries nothing the app needs (copy, move, compress, restore).
    pub(crate) async fn authed_empty(
        &self,
        token: &str,
        method: reqwest::Method,
        route: &str,
        query: &[(&str, &str)],
        body: Option<serde_json::Value>,
    ) -> Result<(), AgentError> {
        let mut req = self.authed_request(token, method, route, query)?;
        if let Some(body) = body {
            req = req.json(&body);
        }
        let resp = req.send().await.map_err(net)?;
        if resp.status().is_success() {
            // Drain a small body so the connection can be reused; its content is not used.
            let _ = read_capped(resp, MAX_JSON_BODY).await;
            return Ok(());
        }
        Err(error_of(resp).await)
    }

    /// A signed-in GET of raw bytes (a preview or a thumbnail), refused when longer than `max`.
    pub(crate) async fn authed_bytes(
        &self,
        token: &str,
        route: &str,
        query: &[(&str, &str)],
        max: usize,
    ) -> Result<Vec<u8>, AgentError> {
        let resp = self
            .authed_request(token, reqwest::Method::GET, route, query)?
            .send()
            .await
            .map_err(net)?;
        if !resp.status().is_success() {
            return Err(error_of(resp).await);
        }
        read_capped(resp, max).await
    }

    /// A signed-in PUT of raw bytes whose answer is JSON (saving a text file).
    pub(crate) async fn authed_put<T: serde::de::DeserializeOwned>(
        &self,
        token: &str,
        route: &str,
        query: &[(&str, &str)],
        body: Vec<u8>,
    ) -> Result<T, AgentError> {
        let resp = self
            .authed_request(token, reqwest::Method::PUT, route, query)?
            .header("Content-Type", "application/octet-stream")
            .body(body)
            .send()
            .await
            .map_err(net)?;
        parse(resp).await
    }
}
// ---- end feature:file-ops ----

// ---- feature:app-catalog ----
impl AgentClient {
    /// One authenticated JSON call for feature modules (`path` is under `/v1`, built by the caller
    /// from validated parts, never from user text). Errors are the same `AgentError`s as above.
    pub(crate) async fn call_json<T: serde::de::DeserializeOwned>(
        &self,
        method: reqwest::Method,
        path: &str,
        token: &str,
    ) -> Result<T, AgentError> {
        parse(
            self.http
                .request(method, format!("{}{path}", self.base))
                .bearer_auth(token)
                .header("X-RFE-Client-Version", CLIENT_VERSION)
                .send()
                .await
                .map_err(net)?,
        )
        .await
    }
}

// ---- feature:health-metrics ----
impl AgentClient {
    /// `GET` one of the agent's fixed, read-only routes as a signed-in device and parses the JSON
    /// answer. `path` is a `'static` literal chosen by the app (never text from the window), and
    /// `timeout` bounds the whole request, so an agent that accepts the connection and then says
    /// nothing, or a PC that fell off the network, ends in an error instead of a wait.
    pub async fn get_fixed_json<T: serde::de::DeserializeOwned>(
        &self,
        token: &str,
        path: &'static str,
        timeout: Duration,
    ) -> Result<T, AgentError> {
        debug_assert!(path.starts_with('/') && !path.contains(['?', '#', ' ']));
        let resp = self
            .http
            .get(format!("{}{path}", self.base))
            .bearer_auth(token)
            .header("X-RFE-Client-Version", CLIENT_VERSION)
            .timeout(timeout)
            .send()
            .await
            .map_err(|e| {
                if e.is_timeout() {
                    let secs = timeout.as_secs().max(1);
                    AgentError::Local(format!(
                        "The agent did not answer within {secs} second{}. It may be asleep, busy \
                         or on another network; check that it is running and the address is right.",
                        if secs == 1 { "" } else { "s" }
                    ))
                } else {
                    net(e)
                }
            })?;
        parse(resp).await
    }
}

// ---- feature:pairing-codes ----
// `POST /pairing/generate`: the agent mints a one-time pairing code for an admin session. Only
// the code and its lifetime are read; the QR payload and picture the agent also sends are
// ignored on purpose (they carry the same code and this app shows only the text).

#[derive(Default, Deserialize)]
#[serde(rename_all = "camelCase", default)]
pub struct GeneratedCode {
    pub pairing_code: String,
    pub expires_in_seconds: u64,
    /// What the phone's scanner reads (address, Tailscale address, certificate fingerprint and
    /// the code). It carries the live code, so it is redacted in `Debug` like the code itself.
    pub qr_payload: serde_json::Value,
}

// Not derived: a `{:?}` in a log line or a test failure must not print a live pairing code.
impl std::fmt::Debug for GeneratedCode {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("GeneratedCode")
            .field("pairing_code", &"<redacted>")
            .field("expires_in_seconds", &self.expires_in_seconds)
            .finish()
    }
}

impl AgentClient {
    /// Asks the agent for a new one-time pairing code that stays valid `ttl_seconds`. Admin
    /// sessions only: any other session gets `FORBIDDEN`.
    pub async fn generate_pairing_code(
        &self,
        token: &str,
        ttl_seconds: u64,
    ) -> Result<GeneratedCode, AgentError> {
        parse(
            self.http
                .post(format!("{}/pairing/generate", self.base))
                .bearer_auth(token)
                .header("X-RFE-Client-Version", CLIENT_VERSION)
                .json(&serde_json::json!({ "ttlSeconds": ttl_seconds }))
                .send()
                .await
                .map_err(net)?,
        )
        .await
    }
}

// ---- feature:device-actions ----
impl AgentClient {
    /// One authenticated JSON call to `/v1{path}` over the pinned connection. `path` is built by
    /// the caller from validated parts and is never user text. A success returns the body as JSON
    /// (`Null` when there is none, as for 204); an error is the agent's `{code, message}`.
    pub(crate) async fn call_device_json(
        &self,
        method: reqwest::Method,
        path: &str,
        token: &str,
        body: Option<&serde_json::Value>,
    ) -> Result<serde_json::Value, AgentError> {
        let mut req = self
            .http
            .request(method, format!("{}{path}", self.base))
            .bearer_auth(token)
            .header("X-RFE-Client-Version", CLIENT_VERSION);
        if let Some(b) = body {
            req = req.json(b);
        }
        let resp = req.send().await.map_err(net)?;
        if !resp.status().is_success() {
            return Err(error_of(resp).await);
        }
        let bytes = read_capped(resp, MAX_JSON_BODY).await?;
        if bytes.is_empty() {
            return Ok(serde_json::Value::Null);
        }
        serde_json::from_slice(&bytes)
            .map_err(|e| AgentError::Local(format!("unexpected response: {e}")))
    }
}

// ---- feature:pair-inbox ----
// The owner's side of approve-on-PC pairing: list the requests waiting at the agent and answer
// them. Admin session only; any other login gets 403 FORBIDDEN. Kept here because it needs the
// pinned client's private parts; the flow around it is in `pair_inbox.rs`.

/// One pairing request waiting for the owner, as the agent lists it. `label` and `remote_ip` come
/// from whoever sent the request, so they are text to show, never to act on.
#[derive(Debug, Clone, Default, Deserialize)]
#[serde(rename_all = "camelCase", default)]
pub struct WaitingPairRequest {
    pub id: String,
    pub label: String,
    pub match_code: String,
    pub remote_ip: String,
    /// Label of the paired device an approval would take over; empty for a new computer.
    pub replaces: String,
    /// RFC 3339, UTC.
    pub expires_at: String,
}

#[derive(Default, Deserialize)]
#[serde(default)]
struct WaitingPairRequests {
    requests: Vec<WaitingPairRequest>,
}

/// Only what the agent makes ids from; nothing else may enter a URL.
fn valid_pair_request_id(id: &str) -> bool {
    !id.is_empty() && id.len() <= 64 && id.chars().all(|c| c.is_ascii_alphanumeric())
}

impl AgentClient {
    /// Lists the requests waiting for the owner (`GET /pair/requests`), oldest first.
    pub async fn waiting_pair_requests(
        &self,
        token: &str,
    ) -> Result<Vec<WaitingPairRequest>, AgentError> {
        let body: WaitingPairRequests = parse(
            self.http
                .get(format!("{}/pair/requests", self.base))
                .bearer_auth(token)
                .header("X-RFE-Client-Version", CLIENT_VERSION)
                .send()
                .await
                .map_err(net)?,
        )
        .await?;
        Ok(body.requests)
    }

    /// Approves or rejects one waiting request (`POST /pair/requests/{id}/approve|reject`).
    /// `NOT_FOUND` means it expired or was answered already, here or on the PC.
    pub async fn answer_pair_request(
        &self,
        token: &str,
        request_id: &str,
        approve: bool,
    ) -> Result<(), AgentError> {
        if !valid_pair_request_id(request_id) {
            return Err(AgentError::Local(format!(
                "unexpected request id {request_id:?}"
            )));
        }
        let verb = if approve { "approve" } else { "reject" };
        let resp = self
            .http
            .post(format!("{}/pair/requests/{request_id}/{verb}", self.base))
            .bearer_auth(token)
            .header("X-RFE-Client-Version", CLIENT_VERSION)
            .send()
            .await
            .map_err(net)?;
        if resp.status().is_success() {
            Ok(())
        } else {
            Err(error_of(resp).await)
        }
    }
}
// ---- end feature:pair-inbox ----

// ---- feature:transfers ----
// What `transfers.rs` needs from this module and cannot reach: the pinned HTTP client and the error
// helpers. Every transfer request still goes through a client built by `AgentClient::pinned`.
impl AgentClient {
    /// The pinned HTTP client and the `https://host:port/v1` base, for requests that stream a body.
    pub(crate) fn transport(&self) -> (&reqwest::Client, &str) {
        (&self.http, &self.base)
    }
}

/// The error for a non-success response (the agent's `{code, message}` body when it has one).
pub(crate) async fn response_error(resp: reqwest::Response) -> AgentError {
    error_of(resp).await
}

/// A transport failure as an `AgentError`, with the whole cause chain in the text.
pub(crate) fn network_error(e: reqwest::Error) -> AgentError {
    net(e)
}

#[cfg(test)]
mod timeout_tests {
    use super::*;
    use reqwest::Method;

    #[test]
    fn every_call_outlasts_the_agents_15_second_budgets() {
        let budget = Duration::from_secs(15);
        for (m, r) in [
            (Method::GET, "/search"),
            (Method::GET, "/fs/recent"),
            (Method::GET, "/fs"),
        ] {
            assert!(route_timeout(&m, r) > budget, "{m} {r}");
        }
    }

    #[test]
    fn whole_tree_operations_get_the_long_limit() {
        for (m, r) in [
            (Method::POST, "/fs/copy"),
            (Method::POST, "/fs/move"),
            (Method::POST, "/fs/compress"),
            (Method::POST, "/fs/extract"),
            (Method::GET, "/fs/archive"),
            (Method::POST, "/fs/checksum"),
            (Method::DELETE, "/trash"),
            (Method::POST, "/trash/restore"),
            (Method::DELETE, "/fs"),
        ] {
            assert_eq!(route_timeout(&m, r), LONG_OP_TIMEOUT, "{m} {r}");
        }
        assert_eq!(route_timeout(&Method::GET, "/fs"), REQUEST_TIMEOUT);
    }

    #[test]
    fn the_built_request_carries_its_routes_limit() {
        let fp = "ab".repeat(32);
        let client = AgentClient::pinned("127.0.0.1:8765", &fp).unwrap();
        for (m, r, want) in [
            (Method::POST, "/fs/copy", LONG_OP_TIMEOUT),
            (Method::GET, "/search", REQUEST_TIMEOUT),
        ] {
            let req = client
                .authed_request("t", m, r, &[])
                .unwrap()
                .build()
                .unwrap();
            assert_eq!(req.timeout(), Some(&want), "{r}");
        }
    }
}

#[cfg(test)]
mod body_cap_tests {
    use super::*;
    use tokio::io::{AsyncReadExt, AsyncWriteExt};

    /// A plain-HTTP server that answers once with `body_len` bytes of `x` and then closes.
    async fn serve(body_len: usize) -> String {
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        tokio::spawn(async move {
            let (mut sock, _) = listener.accept().await.unwrap();
            let mut buf = [0u8; 1024];
            let _ = sock.read(&mut buf).await;
            let head = format!("HTTP/1.1 200 OK\r\ncontent-length: {body_len}\r\n\r\n");
            let _ = sock.write_all(head.as_bytes()).await;
            let _ = sock.write_all(&vec![b'x'; body_len]).await;
        });
        format!("http://{addr}/")
    }

    #[tokio::test]
    async fn a_body_over_the_cap_is_refused_and_one_at_the_cap_is_read() {
        let over = reqwest::get(serve(2_000).await).await.unwrap();
        let err = read_capped(over, 1_000).await.unwrap_err();
        assert!(err.to_string().contains("more data than"), "{err}");
        let ok = reqwest::get(serve(1_000).await).await.unwrap();
        assert_eq!(read_capped(ok, 1_000).await.unwrap().len(), 1_000);
    }
}

#[cfg(test)]
mod proof_tests {
    use super::*;

    #[test]
    fn only_an_agent_that_verifies_the_bound_proof_is_used() {
        let ok = Challenge {
            nonce: "n".into(),
            proof: "v2".into(),
        };
        assert!(require_bound_proof(&ok).is_ok());
        for proof in ["", "v1", "V2", "v3"] {
            let c = Challenge {
                nonce: "n".into(),
                proof: proof.into(),
            };
            let e = require_bound_proof(&c).unwrap_err();
            assert_eq!(e.to_string(), ERR_AGENT_NO_BOUND_PROOF, "{proof:?}");
        }
    }

    #[test]
    fn a_missing_proof_field_parses_as_unsupported() {
        let c: Challenge = serde_json::from_str(r#"{"nonce":"abc"}"#).unwrap();
        assert!(require_bound_proof(&c).is_err());
    }
}

#[cfg(test)]
mod pair_id_tests {
    use super::valid_pair_request_id;

    #[test]
    fn only_alphanumeric_ids_may_enter_a_url() {
        assert!(valid_pair_request_id("abc123"));
        assert!(!valid_pair_request_id(""));
        assert!(!valid_pair_request_id("../x"));
        assert!(!valid_pair_request_id("a/b"));
        assert!(!valid_pair_request_id(&"a".repeat(65)));
    }
}
