//! EXIF orientation, the only EXIF field thumbnails need.

/// The orientation value (1-8) from an EXIF block, `1` when absent or malformed. Accepts the block with or
/// without its `Exif\0\0` prefix.
pub fn orientation(exif: &[u8]) -> u8 {
    let tiff = exif.strip_prefix(b"Exif\0\0").unwrap_or(exif);
    parse(tiff).unwrap_or(1)
}

fn parse(t: &[u8]) -> Option<u8> {
    let little = match t.get(0..2)? {
        b"II" => true,
        b"MM" => false,
        _ => return None,
    };
    let u16_at = |o: usize| -> Option<u16> {
        let b: [u8; 2] = t.get(o..o + 2)?.try_into().ok()?;
        Some(if little {
            u16::from_le_bytes(b)
        } else {
            u16::from_be_bytes(b)
        })
    };
    let u32_at = |o: usize| -> Option<u32> {
        let b: [u8; 4] = t.get(o..o + 4)?.try_into().ok()?;
        Some(if little {
            u32::from_le_bytes(b)
        } else {
            u32::from_be_bytes(b)
        })
    };
    if u16_at(2)? != 42 {
        return None;
    }
    let ifd = u32_at(4)? as usize;
    let count = u16_at(ifd)? as usize;
    for i in 0..count.min(512) {
        let e = ifd.checked_add(2 + i * 12)?;
        if u16_at(e)? == 0x0112 {
            // Type SHORT, count 1: the value sits in the first two bytes of the value field.
            let v = u16_at(e + 8)?;
            return (1..=8).contains(&v).then_some(v as u8);
        }
    }
    None
}

#[cfg(test)]
mod tests {
    use super::*;

    fn block(little: bool, orientation: u16) -> Vec<u8> {
        let mut v = Vec::new();
        v.extend_from_slice(if little { b"II" } else { b"MM" });
        type Enc = (fn(u16) -> [u8; 2], fn(u32) -> [u8; 4]);
        let (u16b, u32b): Enc = if little {
            (u16::to_le_bytes, u32::to_le_bytes)
        } else {
            (u16::to_be_bytes, u32::to_be_bytes)
        };
        v.extend_from_slice(&u16b(42));
        v.extend_from_slice(&u32b(8));
        v.extend_from_slice(&u16b(1));
        v.extend_from_slice(&u16b(0x0112));
        v.extend_from_slice(&u16b(3));
        v.extend_from_slice(&u32b(1));
        v.extend_from_slice(&u16b(orientation));
        v.extend_from_slice(&[0, 0]);
        v.extend_from_slice(&u32b(0));
        v
    }

    #[test]
    fn reads_both_byte_orders_with_and_without_prefix() {
        for o in 1..=8 {
            assert_eq!(orientation(&block(true, o)), o as u8);
            assert_eq!(orientation(&block(false, o)), o as u8);
            let mut p = b"Exif\0\0".to_vec();
            p.extend(block(true, o));
            assert_eq!(orientation(&p), o as u8);
        }
    }

    #[test]
    fn junk_is_orientation_one() {
        assert_eq!(orientation(b""), 1);
        assert_eq!(orientation(b"II*"), 1);
        assert_eq!(orientation(&block(true, 9)), 1);
        assert_eq!(orientation(&[0xff; 64]), 1);
    }
}
