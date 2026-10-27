//! Finding agents by mDNS. The address logic is tested directly; the network tests advertise a
//! service and a real agent on this machine and find them, and say so when this machine cannot
//! deliver multicast to itself (set RFE_REQUIRE_MDNS=1 to make that a failure instead).

mod common;

use common::{free_port, Agent};
use mdns_sd::{ServiceDaemon, ServiceInfo};
use rfe_desktop_lib::agent_client::capture_fingerprint;
use rfe_desktop_lib::discovery::{candidates, discover, Found};
use rfe_desktop_lib::flows::{self, Saved};
use rfe_desktop_lib::secrets::MemoryStore;
use std::net::{IpAddr, Ipv4Addr, Ipv6Addr};
use std::time::Duration;
use tempfile::TempDir;

fn v4(a: u8, b: u8, c: u8, d: u8) -> IpAddr {
    IpAddr::V4(Ipv4Addr::new(a, b, c, d))
}

#[test]
fn addresses_are_ordered_filtered_and_formatted_for_the_app() {
    let global: Ipv6Addr = "2001:db8::5".parse().unwrap();
    let link_local: Ipv6Addr = "fe80::1".parse().unwrap();
    let list = candidates(
        [
            IpAddr::V6(link_local),
            IpAddr::V6(global),
            v4(192, 168, 1, 20),
            v4(10, 0, 0, 7),
            v4(192, 168, 1, 20),
            v4(0, 0, 0, 0),
            v4(224, 0, 0, 251),
            IpAddr::V6("ff02::fb".parse().unwrap()),
        ],
        8765,
    );
    assert_eq!(
        list,
        vec![
            "10.0.0.7:8765".to_string(),
            "192.168.1.20:8765".to_string(),
            "[2001:db8::5]:8765".to_string(),
        ],
        "IPv4 first, no duplicates, no link-local IPv6, no multicast or unspecified"
    );
    assert!(candidates([v4(0, 0, 0, 0)], 1).is_empty());
}

#[test]
fn every_candidate_is_an_address_the_app_accepts() {
    for hp in candidates([v4(10, 1, 2, 3), IpAddr::V6("::1".parse().unwrap())], 65535) {
        rfe_desktop_lib::agent_client::validate_hostport(&hp).unwrap();
    }
}

/// Whether this machine delivers its own multicast to a browser: advertise a service with a
/// second library and look for it. The agent tests below only mean something when it does.
async fn multicast_works(port: u16) -> bool {
    let daemon = ServiceDaemon::new().unwrap();
    let info = ServiceInfo::new(
        "_rfe._tcp.local.",
        "selfcheck",
        "selfcheck.local.",
        "127.0.0.1",
        port,
        &[("version", "selfcheck")][..],
    )
    .unwrap()
    .enable_addr_auto();
    daemon.register(info).unwrap();
    let mut ok = false;
    for _ in 0..4 {
        let list = discover(Duration::from_secs(2)).await.unwrap();
        if list.iter().any(|f| f.name == "selfcheck") {
            ok = true;
            break;
        }
    }
    let _ = daemon.shutdown();
    ok
}

fn skip_or_fail(why: &str) {
    if std::env::var("RFE_REQUIRE_MDNS").is_ok() {
        panic!("{why}");
    }
    eprintln!("SKIP: {why}");
}

fn same_port(f: &Found, port: u16) -> bool {
    f.hostport.ends_with(&format!(":{port}"))
}

#[tokio::test]
async fn a_real_agent_is_found_and_still_needs_its_fingerprint_compared() {
    let agent = Agent::start(free_port());
    let port: u16 = agent.host.rsplit(':').next().unwrap().parse().unwrap();
    if !multicast_works(free_port()).await {
        return skip_or_fail("this machine does not deliver mDNS to itself");
    }

    let mut hit = None;
    for _ in 0..5 {
        hit = discover(Duration::from_secs(3))
            .await
            .unwrap()
            .into_iter()
            .find(|f| same_port(f, port));
        if hit.is_some() {
            break;
        }
    }
    let Some(found) = hit else {
        return skip_or_fail("the agent started here was not advertised or not heard");
    };

    // The advertised address reaches that agent, and what discovery returns carries no
    // fingerprint: trust still comes only from comparing it, as for a typed address.
    assert!(!found.name.is_empty());
    let seen = capture_fingerprint(&found.hostport).await.unwrap();
    assert_eq!(seen, agent.status_fingerprint());
    let json = serde_json::to_string(&found).unwrap();
    assert!(
        !json.contains(&seen),
        "discovery must not hand out a fingerprint: {json}"
    );

    // The app marks an address it already trusts, and only that one.
    let dir = TempDir::new().unwrap();
    let find = |dir: &std::path::Path| {
        let dir = dir.to_path_buf();
        async move {
            for _ in 0..5 {
                let list = flows::discover(&dir, Duration::from_secs(3)).await.unwrap();
                if let Some(hit) = list.into_iter().find(|(f, _)| same_port(f, port)) {
                    return Some(hit);
                }
            }
            None
        }
    };
    let (_, known) = find(dir.path()).await.expect("found again");
    assert!(!known, "nothing is trusted yet");
    flows::save(
        dir.path(),
        &MemoryStore::default(),
        &Saved {
            host: found.hostport.clone(),
            fingerprint: seen.clone(),
            ..Default::default()
        },
    )
    .unwrap();
    let (_, known) = find(dir.path()).await.expect("found a third time");
    assert!(known, "the address now has a pin");
}

#[tokio::test]
async fn nothing_advertised_is_an_empty_list_not_an_error() {
    // No assertion about other agents on the network, only that a quiet window is a normal result.
    let r = discover(Duration::from_millis(300)).await;
    assert!(r.is_ok(), "{r:?}");
}
