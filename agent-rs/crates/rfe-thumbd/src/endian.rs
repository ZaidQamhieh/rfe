//! Little- and big-endian integer reads from a byte slice. `None` when the field runs past the end.

/// The `u16` at `at`, read little-endian (`little`) or big-endian.
pub fn u16_at(d: &[u8], at: usize, little: bool) -> Option<u16> {
    let b: [u8; 2] = d.get(at..at + 2)?.try_into().ok()?;
    Some(if little {
        u16::from_le_bytes(b)
    } else {
        u16::from_be_bytes(b)
    })
}

/// The `u32` at `at`, read little-endian (`little`) or big-endian.
pub fn u32_at(d: &[u8], at: usize, little: bool) -> Option<u32> {
    let b: [u8; 4] = d.get(at..at + 4)?.try_into().ok()?;
    Some(if little {
        u32::from_le_bytes(b)
    } else {
        u32::from_be_bytes(b)
    })
}

/// The `u16` at `at`, little-endian.
pub fn le_u16(d: &[u8], at: usize) -> Option<u16> {
    u16_at(d, at, true)
}

/// The `u32` at `at`, little-endian.
pub fn le_u32(d: &[u8], at: usize) -> Option<u32> {
    u32_at(d, at, true)
}
