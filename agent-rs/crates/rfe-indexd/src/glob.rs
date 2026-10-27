//! Port of Go's `path.Match`, so a search glob means exactly what it means in the Go agent.
//! The Go side validates patterns before sending them; an invalid pattern simply never matches here.

/// Go's `ErrBadPattern`.
#[derive(Debug, PartialEq, Eq)]
pub struct BadPattern;

/// Matches `name` against `pattern` with Go `path.Match` semantics. `Err(BadPattern)` is Go's `ErrBadPattern`.
pub fn matches(pattern: &str, name: &str) -> Result<bool, BadPattern> {
    let mut pattern = pattern.as_bytes();
    let mut name = name.as_bytes();
    'pattern: while !pattern.is_empty() {
        let (star, chunk, rest) = scan_chunk(pattern);
        pattern = rest;
        if star && chunk.is_empty() {
            // A trailing * matches the rest of the name unless it contains a '/'.
            return Ok(!name.contains(&b'/'));
        }
        let first = match_chunk(chunk, name);
        if let Ok(Some(t)) = first {
            if t.is_empty() || !pattern.is_empty() {
                name = t;
                continue;
            }
        }
        if first.is_err() {
            return Err(BadPattern);
        }
        if star {
            let mut i = 0;
            while i < name.len() && name[i] != b'/' {
                match match_chunk(chunk, &name[i + 1..]) {
                    Ok(Some(t)) => {
                        if pattern.is_empty() && !t.is_empty() {
                            i += 1;
                            continue;
                        }
                        name = t;
                        continue 'pattern;
                    }
                    Ok(None) => {}
                    Err(BadPattern) => return Err(BadPattern),
                }
                i += 1;
            }
        }
        // The remainder of the pattern must still be well formed.
        while !pattern.is_empty() {
            let (_, chunk, rest) = scan_chunk(pattern);
            pattern = rest;
            match_chunk(chunk, b"")?;
        }
        return Ok(false);
    }
    Ok(name.is_empty())
}

fn scan_chunk(mut pattern: &[u8]) -> (bool, &[u8], &[u8]) {
    let mut star = false;
    while !pattern.is_empty() && pattern[0] == b'*' {
        pattern = &pattern[1..];
        star = true;
    }
    let mut in_range = false;
    let mut i = 0;
    while i < pattern.len() {
        match pattern[i] {
            b'\\' => {
                if i + 1 < pattern.len() {
                    i += 1;
                }
            }
            b'[' => in_range = true,
            b']' => in_range = false,
            b'*' if !in_range => break,
            _ => {}
        }
        i += 1;
    }
    (star, &pattern[..i], &pattern[i..])
}

/// Decodes one UTF-8 rune; invalid input yields U+FFFD with width 1, like Go's `utf8.DecodeRuneInString`.
fn decode_rune(s: &[u8]) -> (u32, usize) {
    if s.is_empty() {
        return (0xFFFD, 0);
    }
    let width = match s[0] {
        0x00..=0x7F => 1,
        0xC2..=0xDF => 2,
        0xE0..=0xEF => 3,
        0xF0..=0xF4 => 4,
        _ => return (0xFFFD, 1),
    };
    if s.len() < width {
        return (0xFFFD, 1);
    }
    match std::str::from_utf8(&s[..width]) {
        Ok(t) => (t.chars().next().map_or(0xFFFD, |c| c as u32), width),
        Err(_) => (0xFFFD, 1),
    }
}

/// Returns `Ok(Some(rest))` on a match, `Ok(None)` when the chunk does not match, `Err` for a bad pattern.
fn match_chunk<'a>(mut chunk: &[u8], mut s: &'a [u8]) -> Result<Option<&'a [u8]>, BadPattern> {
    let mut failed = false;
    while !chunk.is_empty() {
        if !failed && s.is_empty() {
            failed = true;
        }
        match chunk[0] {
            b'[' => {
                let mut r = 0u32;
                if !failed {
                    let (rune, n) = decode_rune(s);
                    r = rune;
                    s = &s[n..];
                }
                chunk = &chunk[1..];
                let mut negated = false;
                if !chunk.is_empty() && chunk[0] == b'^' {
                    negated = true;
                    chunk = &chunk[1..];
                }
                let mut matched = false;
                let mut nrange = 0;
                loop {
                    if !chunk.is_empty() && chunk[0] == b']' && nrange > 0 {
                        chunk = &chunk[1..];
                        break;
                    }
                    let (lo, rest) = get_esc(chunk)?;
                    chunk = rest;
                    let mut hi = lo;
                    // get_esc guarantees chunk is non-empty here.
                    if chunk[0] == b'-' {
                        let (h, rest) = get_esc(&chunk[1..])?;
                        hi = h;
                        chunk = rest;
                    }
                    if lo <= r && r <= hi {
                        matched = true;
                    }
                    nrange += 1;
                }
                if matched == negated {
                    failed = true;
                }
            }
            b'?' => {
                if !failed {
                    if s[0] == b'/' {
                        failed = true;
                    }
                    let (_, n) = decode_rune(s);
                    s = &s[n..];
                }
                chunk = &chunk[1..];
            }
            b'\\' => {
                chunk = &chunk[1..];
                if chunk.is_empty() {
                    return Err(BadPattern);
                }
                if !failed {
                    if chunk[0] != s[0] {
                        failed = true;
                    }
                    s = &s[1..];
                }
                chunk = &chunk[1..];
            }
            _ => {
                if !failed {
                    if chunk[0] != s[0] {
                        failed = true;
                    }
                    s = &s[1..];
                }
                chunk = &chunk[1..];
            }
        }
    }
    Ok(if failed { None } else { Some(s) })
}

fn get_esc(mut chunk: &[u8]) -> Result<(u32, &[u8]), BadPattern> {
    if chunk.is_empty() || chunk[0] == b'-' || chunk[0] == b']' {
        return Err(BadPattern);
    }
    if chunk[0] == b'\\' {
        chunk = &chunk[1..];
        if chunk.is_empty() {
            return Err(BadPattern);
        }
    }
    let (r, n) = decode_rune(chunk);
    if r == 0xFFFD && n == 1 {
        return Err(BadPattern);
    }
    let rest = &chunk[n..];
    if rest.is_empty() {
        return Err(BadPattern);
    }
    Ok((r, rest))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn basics() {
        assert_eq!(matches("*.txt", "a.txt"), Ok(true));
        assert_eq!(matches("a?c", "abc"), Ok(true));
        assert_eq!(matches("a*b", "a/b"), Ok(false), "* never crosses a slash");
        assert_eq!(matches("[a-c]x", "bx"), Ok(true));
        assert_eq!(matches("[", "a"), Err(BadPattern));
    }

    /// 17k cases generated by the real Go `path.Match` (tools/gen_glob_cases.go): pattern, name, 1/0/E.
    #[test]
    fn agrees_with_go_path_match_on_generated_cases() {
        let data = include_str!("../tests/data/glob_cases.tsv");
        let mut n = 0;
        for line in data.lines() {
            let f: Vec<&str> = line.split('\t').collect();
            assert_eq!(f.len(), 3, "bad case line {line:?}");
            let got = match matches(f[0], f[1]) {
                Ok(true) => "1",
                Ok(false) => "0",
                Err(BadPattern) => "E",
            };
            assert_eq!(got, f[2], "pattern {:?} name {:?}", f[0], f[1]);
            n += 1;
        }
        assert!(n > 10_000);
    }
}
