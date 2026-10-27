//! The command list is kept in three places: the handler in `src/lib.rs`, `commands.txt` (read by
//! build.rs, which gates each command by a permission) and `capabilities/default.json` (which
//! grants them). A command missing from the second is not gated; missing from the third, it fails
//! at run time with "not allowed". This test keeps all three equal.
use std::collections::BTreeSet;

fn handler_names() -> BTreeSet<String> {
    let lib = std::fs::read_to_string("src/lib.rs").unwrap();
    let start = lib.find("generate_handler![").expect("handler list");
    let body = &lib[start + "generate_handler![".len()..];
    let body = &body[..body.find("])").expect("end of handler list")];
    body.lines()
        .map(|l| {
            l.split("//")
                .next()
                .unwrap()
                .trim()
                .trim_end_matches(',')
                .trim()
        })
        .filter(|l| !l.is_empty())
        .map(|l| l.rsplit("::").next().unwrap().to_string())
        .collect()
}

#[test]
fn every_command_is_declared_and_granted() {
    let handlers = handler_names();
    let declared: BTreeSet<String> = std::fs::read_to_string("commands.txt")
        .unwrap()
        .lines()
        .map(|l| l.trim().to_string())
        .filter(|l| !l.is_empty())
        .collect();
    assert_eq!(
        handlers, declared,
        "src/lib.rs handler list and commands.txt differ"
    );

    let cap: serde_json::Value =
        serde_json::from_str(&std::fs::read_to_string("capabilities/default.json").unwrap())
            .unwrap();
    let granted: BTreeSet<String> = cap["permissions"]
        .as_array()
        .unwrap()
        .iter()
        .filter_map(|p| {
            p.as_str()?
                .strip_prefix("allow-")
                .map(|n| n.replace('-', "_"))
        })
        .collect();
    assert_eq!(
        declared, granted,
        "commands.txt and capabilities/default.json differ"
    );
    assert_eq!(cap["windows"], serde_json::json!(["main"]));
}
