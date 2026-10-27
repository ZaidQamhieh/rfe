//! Persistent Ed25519 device identity. The agent pins the public key to the
//! device row at enrollment and checks a signature over a fresh nonce.
//!
//! The private key lives in the OS keystore only. A pre-keystore `identity.json` is moved into
//! the keystore and then deleted.

use crate::applog;
use crate::secrets::{account, SecretStore};
use base64::{engine::general_purpose::STANDARD, Engine};
use ed25519_dalek::{Signer, SigningKey};
use serde::{Deserialize, Serialize};
use std::path::Path;
use std::sync::{Mutex, MutexGuard};

pub struct Identity {
    device_id: String,
    key: SigningKey,
}

#[derive(Serialize, Deserialize)]
struct IdentityFile {
    device_id: String,
    private_key: String,
}

/// One creation or reset at a time: a sign-in and a pairing starting together on a fresh install
/// must end up with the same key, not each their own with the last write winning.
static KEY_LOCK: Mutex<()> = Mutex::new(());

fn key_lock() -> MutexGuard<'static, ()> {
    KEY_LOCK.lock().unwrap_or_else(|p| p.into_inner())
}

fn random<const N: usize>() -> Result<[u8; N], String> {
    let mut buf = [0u8; N];
    getrandom::getrandom(&mut buf).map_err(|e| format!("random: {e}"))?;
    Ok(buf)
}

impl Identity {
    pub fn load_or_create(dir: &Path, store: &dyn SecretStore) -> Result<Self, String> {
        let _one_at_a_time = key_lock();
        let acct = account("identity", dir);
        let legacy_path = dir.join("identity.json");
        let legacy = match std::fs::read(&legacy_path) {
            Ok(bytes) => Some(Self::from_file(&bytes).map_err(|e| {
                format!(
                    "{} is damaged ({e}); delete it to create a new device identity",
                    legacy_path.display()
                )
            })?),
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => None,
            Err(e) => return Err(format!("read {}: {e}", legacy_path.display())),
        };
        let stored = match store.get(&acct)? {
            Some(json) => Some(
                Self::from_file(json.as_bytes())
                    .map_err(|e| format!("the keystore identity is damaged ({e})"))?,
            ),
            None => None,
        };
        match (stored, legacy) {
            (Some(id), None) => Ok(id),
            (Some(id), Some(old)) => {
                // A migration that stopped after the keystore write: finish it, but never
                // delete a key that differs from the one in the keystore.
                if old.key.to_bytes() != id.key.to_bytes() {
                    return Err(format!(
                        "{} holds a different key than the keystore; delete the one you do not want",
                        legacy_path.display()
                    ));
                }
                remove_legacy(&legacy_path)?;
                Ok(id)
            }
            (None, Some(old)) => {
                old.save(store, &acct)?;
                remove_legacy(&legacy_path)?;
                Ok(old)
            }
            (None, None) => {
                let id = Self {
                    device_id: format!("desktop-{}", hex::encode(random::<12>()?)),
                    key: SigningKey::from_bytes(&random::<32>()?),
                };
                applog::register_secret(&STANDARD.encode(id.key.to_bytes()));
                id.save(store, &acct)?;
                Ok(id)
            }
        }
    }

    /// Deletes this computer's device key (and a leftover pre-keystore file), so the next sign-in
    /// creates a new key and a new device id. Only ever called on the user's explicit request:
    /// nothing else in the app removes the key, not even a refused sign-in.
    pub fn reset(dir: &Path, store: &dyn SecretStore) -> Result<(), String> {
        let _one_at_a_time = key_lock();
        let legacy_path = dir.join("identity.json");
        store.delete(&account("identity", dir))?;
        match std::fs::remove_file(&legacy_path) {
            Ok(()) => Ok(()),
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(()),
            Err(e) => Err(format!("remove {}: {e}", legacy_path.display())),
        }
    }

    /// Stores the identity and reads it back, so a keystore that accepts but loses the write
    /// is caught before the legacy file is removed or the key is used.
    fn save(&self, store: &dyn SecretStore, acct: &str) -> Result<(), String> {
        let json = serde_json::to_string(&IdentityFile {
            device_id: self.device_id.clone(),
            private_key: STANDARD.encode(self.key.to_bytes()),
        })
        .map_err(|e| e.to_string())?;
        store.set(acct, &json)?;
        if store.get(acct)?.as_deref() != Some(json.as_str()) {
            return Err("the OS keystore did not keep the device key".into());
        }
        Ok(())
    }

    fn from_file(bytes: &[u8]) -> Result<Self, String> {
        let f: IdentityFile =
            serde_json::from_slice(bytes).map_err(|e| format!("identity.json: {e}"))?;
        let raw = STANDARD
            .decode(&f.private_key)
            .map_err(|e| format!("identity key: {e}"))?;
        let arr: [u8; 32] = raw
            .try_into()
            .map_err(|_| "identity key must be 32 bytes".to_string())?;
        applog::register_secret(&f.private_key);
        Ok(Self {
            device_id: f.device_id,
            key: SigningKey::from_bytes(&arr),
        })
    }

    pub fn device_id(&self) -> &str {
        &self.device_id
    }

    pub fn public_key_b64(&self) -> String {
        STANDARD.encode(self.key.verifying_key().to_bytes())
    }

    /// The proof the app sends: a signature over the agent's certificate fingerprint and the nonce
    /// (`rfe-device-proof-v2`), so it is worthless at any other agent. A malicious agent that fetched a
    /// nonce from a second agent and presented it as its own challenge would get a signature that
    /// names the first agent's certificate, and the second one rejects it.
    pub fn proof_b64(&self, agent_fingerprint: &str, nonce: &str) -> String {
        self.sign_b64(&format!(
            "rfe-device-proof-v2\n{}\n{nonce}",
            agent_fingerprint.to_ascii_lowercase()
        ))
    }

    /// Signs the message's UTF-8 bytes as they are. The app never signs a bare nonce (use
    /// [`Identity::proof_b64`]); this is public for tests that build requests the app would not.
    #[doc(hidden)]
    pub fn sign_b64(&self, message: &str) -> String {
        STANDARD.encode(self.key.sign(message.as_bytes()).to_bytes())
    }
}

fn remove_legacy(path: &Path) -> Result<(), String> {
    std::fs::remove_file(path).map_err(|e| format!("remove {}: {e}", path.display()))
}
