//! Finding agents on the local network by mDNS/DNS-SD (`_rfe._tcp`).
//!
//! Discovery only produces addresses to try. It proves nothing: anyone on the network can
//! advertise this service name, so a found agent is exactly as untrusted as an address typed by
//! hand, and goes through the same certificate comparison before any credential is sent. This
//! module returns data and touches no secret and no connection to an agent.

use mdns_sd::{ServiceDaemon, ServiceEvent};
use serde::Serialize;
use std::collections::BTreeMap;
use std::net::IpAddr;
use std::time::{Duration, Instant};

pub const SERVICE: &str = "_rfe._tcp.local.";

/// Most agents listed; a network can be made to advertise any number.
const MAX_FOUND: usize = 50;
/// Entries tracked while browsing, and how many one network address may account for: a peer that
/// advertises many names cannot push the real agents out of the list (audit F-171).
const MAX_TRACKED: usize = 200;
const MAX_PER_ADDRESS: usize = 3;

/// One advertised agent.
#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct Found {
    /// The instance name the agent gave itself; display only.
    pub name: String,
    /// The address to try first, as `host:port`.
    pub hostport: String,
    /// Its other addresses, if it advertised several.
    pub other_addresses: Vec<String>,
    /// The version it advertised; display only.
    pub version: String,
}

/// Text from the network, made safe to show: no control characters, bounded length.
fn display(s: &str, max: usize) -> String {
    // Control characters and angle brackets become spaces: a name that arrives
    // over the network is shown in the interface and must never look like markup.
    s.chars()
        .map(|c| {
            if c.is_control() || c == '<' || c == '>' {
                ' '
            } else {
                c
            }
        })
        .take(max)
        .collect::<String>()
        .trim()
        .to_string()
}

fn is_link_local_v6(a: &std::net::Ipv6Addr) -> bool {
    (a.segments()[0] & 0xffc0) == 0xfe80
}

/// The `host:port` strings worth trying for these addresses, best first: IPv4 before IPv6, no
/// unspecified or multicast addresses, and no IPv6 link-local address (it cannot be used without
/// a zone, which the app does not accept). Every result passes the app's address check.
pub fn candidates(addrs: impl IntoIterator<Item = IpAddr>, port: u16) -> Vec<String> {
    let mut v4 = Vec::new();
    let mut v6 = Vec::new();
    for a in addrs {
        if a.is_unspecified() || a.is_multicast() {
            continue;
        }
        match a {
            IpAddr::V4(a) => v4.push(a),
            IpAddr::V6(a) if !is_link_local_v6(&a) => v6.push(a),
            IpAddr::V6(_) => {}
        }
    }
    v4.sort();
    v4.dedup();
    v6.sort();
    v6.dedup();
    let all = v4
        .into_iter()
        .map(|a| format!("{a}:{port}"))
        .chain(v6.into_iter().map(|a| format!("[{a}]:{port}")));
    all.filter(|hp| crate::agent_client::validate_hostport(hp).is_ok())
        .collect()
}

/// "pc._rfe._tcp.local." -> "pc"
fn instance_name(fullname: &str) -> &str {
    fullname
        .strip_suffix(SERVICE)
        .map(|n| n.trim_end_matches('.'))
        .unwrap_or(fullname)
}

/// Browses for `window` and returns what answered. An empty list is a normal result (no agent,
/// or a network that blocks multicast); an error means the browser itself could not start.
pub async fn discover(window: Duration) -> Result<Vec<Found>, String> {
    tokio::task::spawn_blocking(move || browse(window))
        .await
        .map_err(|e| format!("network discovery stopped: {e}"))?
}

fn browse(window: Duration) -> Result<Vec<Found>, String> {
    let daemon =
        ServiceDaemon::new().map_err(|e| format!("cannot start network discovery: {e}"))?;
    let events = daemon
        .browse(SERVICE)
        .map_err(|e| format!("cannot start network discovery: {e}"))?;
    let deadline = Instant::now() + window;
    let mut found: BTreeMap<String, Found> = BTreeMap::new();
    while let Some(left) = deadline.checked_duration_since(Instant::now()) {
        match events.recv_timeout(left) {
            Ok(ServiceEvent::ServiceResolved(r)) => {
                let mut list = candidates(r.addresses.iter().map(|a| a.to_ip_addr()), r.port);
                if list.is_empty() {
                    continue;
                }
                let first = list.remove(0);
                if !found.contains_key(&r.fullname) {
                    if let Some(victim) = crowded_victim(&found, &first) {
                        found.remove(&victim);
                    }
                }
                if !admit(&found, &r.fullname, &first) {
                    continue;
                }
                found.insert(
                    r.fullname.clone(),
                    Found {
                        name: display(instance_name(&r.fullname), 64),
                        hostport: first,
                        other_addresses: list,
                        version: display(
                            r.txt_properties
                                .get_property_val_str("version")
                                .unwrap_or(""),
                            32,
                        ),
                    },
                );
            }
            Ok(_) => {}
            Err(_) => break,
        }
    }
    let _ = daemon.shutdown();
    Ok(found.into_values().take(MAX_FOUND).collect())
}

fn address_of(hostport: &str) -> &str {
    hostport.rsplit_once(':').map_or(hostport, |(a, _)| a)
}

/// When the list is full, the name to drop so a newcomer from a less crowded address still fits: one of the entries
/// of the address that holds the most (a flood from a few hosts cannot push the real agent out, audit F-15).
fn crowded_victim(found: &BTreeMap<String, Found>, hostport: &str) -> Option<String> {
    if found.len() < MAX_TRACKED {
        return None;
    }
    let mut per: BTreeMap<&str, usize> = BTreeMap::new();
    for f in found.values() {
        *per.entry(address_of(&f.hostport)).or_default() += 1;
    }
    let mine = per.get(address_of(hostport)).copied().unwrap_or(0);
    let (crowded, most) = per.iter().max_by_key(|(_, n)| **n).map(|(a, n)| (*a, *n))?;
    if most <= mine + 1 {
        return None;
    }
    found
        .iter()
        .find(|(_, f)| address_of(&f.hostport) == crowded)
        .map(|(name, _)| name.clone())
}

/// Whether a resolved service may join the list. A name already listed always updates.
fn admit(found: &BTreeMap<String, Found>, fullname: &str, hostport: &str) -> bool {
    if found.contains_key(fullname) {
        return true;
    }
    if found.len() >= MAX_TRACKED {
        return false;
    }
    let from_here = found
        .values()
        .filter(|f| address_of(&f.hostport) == address_of(hostport))
        .count();
    from_here < MAX_PER_ADDRESS
}

#[cfg(test)]
mod admit_tests {
    use super::*;

    fn entry(hostport: &str) -> Found {
        Found {
            name: "n".into(),
            hostport: hostport.into(),
            other_addresses: vec![],
            version: String::new(),
        }
    }

    #[test]
    fn one_address_cannot_fill_the_list() {
        let mut found = BTreeMap::new();
        for i in 0..MAX_PER_ADDRESS {
            let name = format!("a{i}._rfe._tcp.local.");
            assert!(admit(&found, &name, "10.0.0.9:8765"));
            found.insert(name, entry("10.0.0.9:8765"));
        }
        assert!(!admit(&found, "a-extra._rfe._tcp.local.", "10.0.0.9:8765"));
        assert!(admit(&found, "real._rfe._tcp.local.", "10.0.0.2:8765"));
        assert!(
            admit(&found, "a0._rfe._tcp.local.", "10.0.0.9:8765"),
            "a listed name still updates"
        );
    }
}

#[cfg(test)]
mod flood_tests {
    use super::*;

    #[test]
    fn a_full_list_makes_room_for_a_new_address_by_dropping_the_most_crowded_one() {
        let mut found = BTreeMap::new();
        for i in 0..MAX_TRACKED {
            // 67 spoofing addresses with three names each, so the list is full
            let addr = format!("10.0.{}.{}:8765", i / 3 / 200, i / 3 % 200 + 1);
            found.insert(
                format!("f{i}._rfe._tcp.local."),
                Found {
                    name: "n".into(),
                    hostport: addr,
                    other_addresses: vec![],
                    version: String::new(),
                },
            );
        }
        // Even when every address holds three, an address with none gets in by displacing one of them.
        assert!(crowded_victim(&found, "192.168.1.5:8765").is_some());
        // An address that already holds as many as the most crowded one displaces nobody.
        assert_eq!(crowded_victim(&found, "10.0.0.1:8765"), None);
        // One address now holds more than the rest: it is the one that gives up an entry.
        for i in 0..5 {
            found.insert(
                format!("x{i}._rfe._tcp.local."),
                Found {
                    name: "n".into(),
                    hostport: "10.9.9.9:8765".into(),
                    other_addresses: vec![],
                    version: String::new(),
                },
            );
        }
        let victim = crowded_victim(&found, "192.168.1.5:8765").expect("a victim");
        assert_eq!(address_of(&found[&victim].hostport), "10.9.9.9");
    }
}

#[cfg(test)]
mod display_tests {
    use super::display;

    #[test]
    fn network_names_never_carry_markup_or_controls() {
        assert_eq!(display("<img src=x onerror=1>", 64), "img src=x onerror=1");
        assert_eq!(display("a\u{0007}b\nc", 64), "a b c");
    }
}
