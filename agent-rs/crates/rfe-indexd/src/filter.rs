//! Query filters with the same semantics as the Go agent's `searchFilters`.

use crate::glob;
use serde::Deserialize;
use std::collections::HashSet;

const CATEGORY_EXTENSIONS: &[(&str, &[&str])] = &[
    (
        "image",
        &[
            "jpg", "jpeg", "png", "gif", "webp", "bmp", "heic", "heif", "svg", "avif", "tiff",
        ],
    ),
    (
        "video",
        &[
            "mp4", "mkv", "avi", "mov", "wmv", "flv", "webm", "m4v", "3gp", "mpg", "mpeg",
        ],
    ),
    (
        "audio",
        &[
            "mp3", "wav", "flac", "ogg", "m4a", "aac", "wma", "opus", "mid",
        ],
    ),
    (
        "document",
        &[
            "pdf", "doc", "docx", "xls", "xlsx", "ppt", "pptx", "txt", "md", "odt", "ods", "odp",
            "rtf", "csv", "epub",
        ],
    ),
    (
        "archive",
        &[
            "zip", "rar", "7z", "tar", "gz", "bz2", "xz", "zst", "iso", "jar",
        ],
    ),
];

/// Lowercase, dot-free extension as Go's `filepath.Ext` defines it: from the last '.' of the name, even at index 0.
pub fn ext_of(name: &str) -> String {
    match name.rfind('.') {
        Some(i) => name[i + 1..].to_lowercase(),
        None => String::new(),
    }
}

/// "folder" for directories, a category for known extensions, "other" otherwise.
pub fn category_for(name: &str, is_dir: bool) -> &'static str {
    if is_dir {
        return "folder";
    }
    let ext = ext_of(name);
    for (cat, exts) in CATEGORY_EXTENSIONS {
        if exts.contains(&ext.as_str()) {
            return cat;
        }
    }
    "other"
}

/// Filters as sent by the Go agent, which has already validated them (glob syntax, category names).
#[derive(Debug, Default, Deserialize)]
#[serde(rename_all = "camelCase", default)]
pub struct Filters {
    pub glob: String,
    pub needle: String,
    pub types: Option<Vec<String>>,
    pub exts: Option<Vec<String>>,
    pub min_size: Option<i64>,
    pub max_size: Option<i64>,
    pub mod_after_ns: Option<i64>,
    pub mod_before_ns: Option<i64>,
}

#[derive(Debug)]
pub struct Compiled {
    glob: String,
    needle: String,
    needle_ascii: bool,
    types: Option<HashSet<String>>,
    exts: Option<HashSet<String>>,
    min_size: Option<i64>,
    max_size: Option<i64>,
    mod_after_ns: Option<i64>,
    mod_before_ns: Option<i64>,
}

impl Compiled {
    pub fn new(f: Filters) -> Self {
        let needle = f.needle.to_lowercase();
        Compiled {
            glob: f.glob.to_lowercase(),
            needle_ascii: needle.is_ascii(),
            needle,
            types: f.types.map(|v| v.into_iter().collect()),
            exts: f.exts.map(|v| v.into_iter().collect()),
            min_size: f.min_size,
            max_size: f.max_size,
            mod_after_ns: f.mod_after_ns,
            mod_before_ns: f.mod_before_ns,
        }
    }

    /// Name filter (glob or substring), case-insensitive.
    pub fn match_name(&self, name: &str) -> bool {
        if name.bytes().all(|b| !b.is_ascii_uppercase()) && name.is_ascii() {
            return self.match_lower(name);
        }
        self.match_lower(&name.to_lowercase())
    }

    /// `match_name` for the query hot path: `ascii` says the name is pure ASCII (known from the index), so it is
    /// compared without allocating; `buf` is scratch space reused across calls.
    pub fn match_name_fast(&self, name: &str, ascii: bool, buf: &mut String) -> bool {
        if !ascii {
            return self.match_lower(&name.to_lowercase());
        }
        if self.glob.is_empty() && self.needle_ascii {
            return contains_ignore_ascii_case(name.as_bytes(), self.needle.as_bytes());
        }
        buf.clear();
        buf.push_str(name);
        buf.make_ascii_lowercase();
        self.match_lower(buf)
    }

    fn match_lower(&self, lower: &str) -> bool {
        if !self.glob.is_empty() {
            return glob::matches(&self.glob, lower) == Ok(true);
        }
        lower.contains(self.needle.as_str())
    }

    /// Everything except the name: types, extensions, size and modified bounds, all ANDed.
    pub fn match_attrs(&self, name: &str, is_dir: bool, size: u64, mtime_ns: i64) -> bool {
        if let Some(types) = &self.types {
            if !types.contains(category_for(name, is_dir)) {
                return false;
            }
        }
        if let Some(exts) = &self.exts {
            if is_dir || !exts.contains(&ext_of(name)) {
                return false;
            }
        }
        if self.min_size.is_some() || self.max_size.is_some() {
            if is_dir {
                return false;
            }
            let size = size as i128;
            if self.min_size.is_some_and(|m| size < m as i128)
                || self.max_size.is_some_and(|m| size > m as i128)
            {
                return false;
            }
        }
        if self.mod_after_ns.is_some_and(|t| mtime_ns < t)
            || self.mod_before_ns.is_some_and(|t| mtime_ns > t)
        {
            return false;
        }
        true
    }
}

/// `haystack` contains `needle` (already lowercase ASCII), ignoring ASCII case.
fn contains_ignore_ascii_case(haystack: &[u8], needle: &[u8]) -> bool {
    let Some((&first, rest)) = needle.split_first() else {
        return true;
    };
    if needle.len() > haystack.len() {
        return false;
    }
    let last_start = haystack.len() - needle.len();
    (0..=last_start).any(|i| {
        haystack[i].to_ascii_lowercase() == first
            && haystack[i + 1..i + needle.len()]
                .iter()
                .zip(rest)
                .all(|(h, n)| h.to_ascii_lowercase() == *n)
    })
}

/// A root is matched by exact path or by the path-boundary prefix, exactly like Go's `underAnyRootScopes`.
pub struct RootScopes(Vec<(String, String)>);

impl RootScopes {
    pub fn new(roots: &[String]) -> Self {
        let sep = std::path::MAIN_SEPARATOR;
        RootScopes(
            roots
                .iter()
                .map(|r| {
                    let prefix = format!("{}{}", r.strip_suffix(sep).unwrap_or(r), sep);
                    (r.clone(), prefix)
                })
                .collect(),
        )
    }

    pub fn contains(&self, path: &str) -> bool {
        self.0
            .iter()
            .any(|(root, prefix)| path == root || path.starts_with(prefix.as_str()))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn compiled(f: Filters) -> Compiled {
        Compiled::new(f)
    }

    #[test]
    fn extension_follows_go_filepath_ext() {
        assert_eq!(ext_of("a.TXT"), "txt");
        assert_eq!(ext_of(".bashrc"), "bashrc");
        assert_eq!(ext_of("a."), "");
        assert_eq!(ext_of("noext"), "");
        assert_eq!(ext_of("a.tar.gz"), "gz");
    }

    #[test]
    fn categories() {
        assert_eq!(category_for("x.JPG", false), "image");
        assert_eq!(category_for("x", true), "folder");
        assert_eq!(category_for("x.weird", false), "other");
    }

    #[test]
    fn name_filter_is_case_insensitive_substring_or_glob() {
        let f = compiled(Filters {
            needle: "Report".into(),
            ..Default::default()
        });
        assert!(f.match_name("annual-REPORT.pdf"));
        assert!(!f.match_name("summary.pdf"));
        let g = compiled(Filters {
            glob: "IMG_*.JPG".into(),
            ..Default::default()
        });
        assert!(g.match_name("img_0001.jpg"));
        assert!(!g.match_name("photo.jpg"));
    }

    #[test]
    fn size_bounds_exclude_directories_and_modified_bounds_apply() {
        let f = compiled(Filters {
            min_size: Some(10),
            max_size: Some(20),
            ..Default::default()
        });
        assert!(f.match_attrs("a", false, 10, 0));
        assert!(!f.match_attrs("a", false, 21, 0));
        assert!(
            !f.match_attrs("a", true, 15, 0),
            "directories never satisfy a size bound"
        );
        let m = compiled(Filters {
            mod_after_ns: Some(100),
            mod_before_ns: Some(200),
            ..Default::default()
        });
        assert!(m.match_attrs("a", false, 0, 150));
        assert!(!m.match_attrs("a", false, 0, 99));
        assert!(!m.match_attrs("a", false, 0, 201));
    }

    #[test]
    fn ext_filter_rejects_directories_and_types_use_categories() {
        let f = compiled(Filters {
            exts: Some(vec!["png".into()]),
            ..Default::default()
        });
        assert!(f.match_attrs("a.PNG", false, 0, 0));
        assert!(!f.match_attrs("a.png", true, 0, 0));
        let t = compiled(Filters {
            types: Some(vec!["folder".into()]),
            ..Default::default()
        });
        assert!(t.match_attrs("d", true, 0, 0));
        assert!(!t.match_attrs("a.png", false, 0, 0));
    }

    #[test]
    fn root_scopes_respect_path_boundaries() {
        let s = RootScopes::new(&["/data".to_string()]);
        assert!(s.contains("/data"));
        assert!(s.contains("/data/a/b"));
        assert!(
            !s.contains("/data2/a"),
            "a sibling that merely shares a prefix is outside"
        );
        assert!(!s.contains("/dat"));
        let slash = RootScopes::new(&["/".to_string()]);
        assert!(slash.contains("/anything"));
    }
}
