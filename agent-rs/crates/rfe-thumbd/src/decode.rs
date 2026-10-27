//! Decoding into 8-bit RGB with hard limits. Transparency is flattened onto black, which is what the Go renderer's
//! JPEG encoder does with alpha, so thumbnails match.

use crate::exif;
use std::io::Cursor;

/// Largest decoded image, in pixels (the Go renderer's budget).
pub const MAX_PIXELS: u64 = 40_000_000;

#[derive(Debug)]
pub enum DecodeError {
    /// Not an image this process can decode (the agent falls back to its own decoder).
    Unsupported(String),
    /// Over the pixel budget.
    TooLarge(String),
}

impl DecodeError {
    fn unsupported(e: impl std::fmt::Display) -> Self {
        DecodeError::Unsupported(e.to_string())
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Format {
    Jpeg,
    Png,
    Gif,
    Webp,
    Tiff,
    Bmp,
}

impl Format {
    pub fn name(self) -> &'static str {
        match self {
            Format::Jpeg => "jpeg",
            Format::Png => "png",
            Format::Gif => "gif",
            Format::Webp => "webp",
            Format::Tiff => "tiff",
            Format::Bmp => "bmp",
        }
    }
}

pub fn sniff(data: &[u8]) -> Option<Format> {
    if data.starts_with(&[0xff, 0xd8, 0xff]) {
        Some(Format::Jpeg)
    } else if data.starts_with(&[0x89, b'P', b'N', b'G', 0x0d, 0x0a, 0x1a, 0x0a]) {
        Some(Format::Png)
    } else if data.starts_with(b"GIF87a") || data.starts_with(b"GIF89a") {
        Some(Format::Gif)
    } else if data.len() >= 12 && &data[0..4] == b"RIFF" && &data[8..12] == b"WEBP" {
        Some(Format::Webp)
    } else if data.starts_with(b"II*\0") || data.starts_with(b"MM\0*") {
        Some(Format::Tiff)
    } else if data.starts_with(b"BM") {
        Some(Format::Bmp)
    } else {
        None
    }
}

#[derive(Debug)]
pub struct Decoded {
    pub format: Format,
    /// Dimensions of the source image as stored.
    pub src_width: u32,
    pub src_height: u32,
    /// Dimensions of `rgb` (smaller than the source when the decoder scaled the JPEG down).
    pub width: u32,
    pub height: u32,
    pub rgb: Vec<u8>,
    /// EXIF orientation, 1-8.
    pub orientation: u8,
}

fn check_pixels(w: u64, h: u64) -> Result<(), DecodeError> {
    if w == 0 || h == 0 {
        return Err(DecodeError::Unsupported(format!(
            "invalid image dimensions {w}x{h}"
        )));
    }
    if w * h > MAX_PIXELS {
        return Err(DecodeError::TooLarge(format!(
            "{w}x{h} exceeds the pixel budget"
        )));
    }
    Ok(())
}

/// Decodes `data`. `want` is the longest side the caller will end up with after fitting; a JPEG may be decoded at a
/// reduced size (never below twice that, so the resize still has real data to work with).
pub fn decode(data: &[u8], want: u32) -> Result<Decoded, DecodeError> {
    match sniff(data) {
        Some(Format::Jpeg) => jpeg(data, want),
        Some(Format::Png) => png(data),
        Some(Format::Gif) => gif(data),
        Some(Format::Webp) => webp(data),
        Some(Format::Tiff) => tiff(data),
        Some(Format::Bmp) => bmp(data),
        None => Err(DecodeError::Unsupported("not a supported image".into())),
    }
}

fn jpeg(data: &[u8], want: u32) -> Result<Decoded, DecodeError> {
    let mut dec = jpeg_decoder::Decoder::new(Cursor::new(data));
    dec.read_info().map_err(DecodeError::unsupported)?;
    let info = dec
        .info()
        .ok_or_else(|| DecodeError::Unsupported("no image info".into()))?;
    let (sw, sh) = (info.width as u32, info.height as u32);
    check_pixels(sw as u64, sh as u64)?;
    let orientation = dec.exif_data().map(exif::orientation).unwrap_or(1);

    // Ask for twice the final size: the DCT scale only ever halves, and Lanczos does the rest.
    let long = sw.max(sh);
    if want > 0 && long > want {
        let f = (want as f64 * 2.0 / long as f64).min(1.0);
        let rw = ((sw as f64 * f).ceil() as u32).clamp(1, u16::MAX as u32) as u16;
        let rh = ((sh as f64 * f).ceil() as u32).clamp(1, u16::MAX as u32) as u16;
        dec.scale(rw, rh).map_err(DecodeError::unsupported)?;
    }
    let pixels = dec.decode().map_err(DecodeError::unsupported)?;
    let info = dec
        .info()
        .ok_or_else(|| DecodeError::Unsupported("no image info".into()))?;
    let (w, h) = (info.width as u32, info.height as u32);
    let n = (w as usize) * (h as usize);
    let rgb = match info.pixel_format {
        jpeg_decoder::PixelFormat::RGB24 => pixels,
        jpeg_decoder::PixelFormat::L8 => pixels.iter().flat_map(|&g| [g, g, g]).collect(),
        jpeg_decoder::PixelFormat::L16 => pixels
            .as_chunks::<2>()
            .0
            .iter()
            .flat_map(|c| [c[0], c[0], c[0]])
            .collect(),
        jpeg_decoder::PixelFormat::CMYK32 => {
            let mut out = Vec::with_capacity(n * 3);
            for c in pixels.as_chunks::<4>().0 {
                // The decoder returns ink amounts (0 = no ink), the same values Go's image.CMYK holds.
                let k = 255 - c[3] as u32;
                out.extend_from_slice(&[
                    ((255 - c[0] as u32) * k / 255) as u8,
                    ((255 - c[1] as u32) * k / 255) as u8,
                    ((255 - c[2] as u32) * k / 255) as u8,
                ]);
            }
            out
        }
    };
    if rgb.len() != n * 3 {
        return Err(DecodeError::Unsupported("unexpected decoded size".into()));
    }
    Ok(Decoded {
        format: Format::Jpeg,
        src_width: sw,
        src_height: sh,
        width: w,
        height: h,
        rgb,
        orientation,
    })
}

/// Flattens RGBA onto black (premultiplying), like the Go JPEG encoder does.
fn flatten_rgba(rgba: &[u8]) -> Vec<u8> {
    let mut out = Vec::with_capacity(rgba.len() / 4 * 3);
    for p in rgba.as_chunks::<4>().0 {
        let a = p[3] as u32;
        out.extend_from_slice(&[
            ((p[0] as u32 * a + 127) / 255) as u8,
            ((p[1] as u32 * a + 127) / 255) as u8,
            ((p[2] as u32 * a + 127) / 255) as u8,
        ]);
    }
    out
}

fn png(data: &[u8]) -> Result<Decoded, DecodeError> {
    let mut dec = png::Decoder::new(Cursor::new(data));
    dec.set_transformations(png::Transformations::EXPAND | png::Transformations::STRIP_16);
    let mut reader = dec.read_info().map_err(DecodeError::unsupported)?;
    let (w, h) = {
        let i = reader.info();
        (i.width, i.height)
    };
    check_pixels(w as u64, h as u64)?;
    let mut buf = vec![0u8; reader.output_buffer_size()];
    let frame = reader
        .next_frame(&mut buf)
        .map_err(DecodeError::unsupported)?;
    let buf = &buf[..frame.buffer_size()];
    let rgb = match frame.color_type {
        png::ColorType::Rgb => buf.to_vec(),
        png::ColorType::Rgba => flatten_rgba(buf),
        png::ColorType::Grayscale => buf.iter().flat_map(|&g| [g, g, g]).collect(),
        png::ColorType::GrayscaleAlpha => buf
            .as_chunks::<2>()
            .0
            .iter()
            .flat_map(|c| {
                let v = ((c[0] as u32 * c[1] as u32 + 127) / 255) as u8;
                [v, v, v]
            })
            .collect(),
        png::ColorType::Indexed => {
            return Err(DecodeError::Unsupported("unexpanded palette".into()))
        }
    };
    Ok(Decoded {
        format: Format::Png,
        src_width: w,
        src_height: h,
        width: w,
        height: h,
        rgb,
        orientation: 1,
    })
}

fn gif(data: &[u8]) -> Result<Decoded, DecodeError> {
    let mut opts = gif::DecodeOptions::new();
    opts.set_color_output(gif::ColorOutput::RGBA);
    // One RGBA frame at the pixel budget; a frame descriptor claiming more is refused before it is allocated
    // (audit F-161).
    opts.set_memory_limit(gif::MemoryLimit::Bytes(
        std::num::NonZeroU64::new(MAX_PIXELS * 4).expect("non-zero"),
    ));
    let mut dec = opts
        .read_info(Cursor::new(data))
        .map_err(DecodeError::unsupported)?;
    check_pixels(dec.width() as u64, dec.height() as u64)?;
    let frame = dec
        .read_next_frame()
        .map_err(|e| match e {
            // The memory limit refused an over-budget frame: that is "too large", which the agent reports as
            // such, not "unsupported", which would send the same input to the unsandboxed in-process decoder.
            gif::DecodingError::Io(ref io) if io.kind() == std::io::ErrorKind::OutOfMemory => {
                DecodeError::TooLarge("gif frame exceeds the memory budget".into())
            }
            e => DecodeError::unsupported(e),
        })?
        .ok_or_else(|| DecodeError::Unsupported("gif without frames".into()))?;
    // Like Go's decoder, the thumbnail is the first frame's own rectangle.
    let (w, h) = (frame.width as u32, frame.height as u32);
    check_pixels(w as u64, h as u64)?;
    Ok(Decoded {
        format: Format::Gif,
        src_width: w,
        src_height: h,
        width: w,
        height: h,
        rgb: flatten_rgba(&frame.buffer),
        orientation: 1,
    })
}

fn webp(data: &[u8]) -> Result<Decoded, DecodeError> {
    let mut dec =
        image_webp::WebPDecoder::new(Cursor::new(data)).map_err(DecodeError::unsupported)?;
    let (w, h) = dec.dimensions();
    check_pixels(w as u64, h as u64)?;
    let size = dec
        .output_buffer_size()
        .ok_or_else(|| DecodeError::TooLarge("webp output size overflows".into()))?;
    let mut buf = vec![0u8; size];
    dec.read_image(&mut buf).map_err(DecodeError::unsupported)?;
    let rgb = if dec.has_alpha() {
        flatten_rgba(&buf)
    } else {
        buf
    };
    Ok(Decoded {
        format: Format::Webp,
        src_width: w,
        src_height: h,
        width: w,
        height: h,
        rgb,
        orientation: 1,
    })
}

/// Scratch memory the TIFF decoder may use on top of the pixel budget.
const TIFF_BUFFER_LIMIT: usize = 256 << 20;

fn tiff(data: &[u8]) -> Result<Decoded, DecodeError> {
    use ::tiff::decoder::{Decoder, DecodingResult, Limits};
    use ::tiff::ColorType;
    let mut limits = Limits::default();
    limits.decoding_buffer_size = TIFF_BUFFER_LIMIT;
    let mut dec = Decoder::new(Cursor::new(data))
        .map_err(DecodeError::unsupported)?
        .with_limits(limits);
    let (w, h) = dec.dimensions().map_err(DecodeError::unsupported)?;
    check_pixels(w as u64, h as u64)?;
    let color = dec.colortype().map_err(DecodeError::unsupported)?;
    let image = dec.read_image().map_err(DecodeError::unsupported)?;
    // 16-bit samples keep their high byte.
    let eight: Vec<u8> = match image {
        DecodingResult::U8(v) => v,
        DecodingResult::U16(v) => v.into_iter().map(|x| (x >> 8) as u8).collect(),
        _ => return Err(DecodeError::Unsupported("tiff sample type".into())),
    };
    let rgb = match color {
        ColorType::Gray(8) | ColorType::Gray(16) => eight.iter().flat_map(|&g| [g, g, g]).collect(),
        ColorType::GrayA(8) | ColorType::GrayA(16) => eight
            .as_chunks::<2>()
            .0
            .iter()
            .flat_map(|c| {
                let v = ((c[0] as u32 * c[1] as u32 + 127) / 255) as u8;
                [v, v, v]
            })
            .collect(),
        ColorType::RGB(8) | ColorType::RGB(16) => eight,
        ColorType::RGBA(8) | ColorType::RGBA(16) => flatten_rgba(&eight),
        other => {
            return Err(DecodeError::Unsupported(format!(
                "tiff color type {other:?}"
            )))
        }
    };
    if rgb.len() != w as usize * h as usize * 3 {
        return Err(DecodeError::Unsupported("tiff pixel count mismatch".into()));
    }
    Ok(Decoded {
        format: Format::Tiff,
        src_width: w,
        src_height: h,
        width: w,
        height: h,
        rgb,
        orientation: 1,
    })
}

fn le_u16(d: &[u8], at: usize) -> Option<u16> {
    Some(u16::from_le_bytes(d.get(at..at + 2)?.try_into().ok()?))
}

fn le_u32(d: &[u8], at: usize) -> Option<u32> {
    Some(u32::from_le_bytes(d.get(at..at + 4)?.try_into().ok()?))
}

/// Uncompressed Windows bitmaps (1, 4, 8, 24 and 32 bits per pixel), like Go's x/image/bmp: no RLE, no bit masks,
/// the alpha of 32-bit pixels is ignored. Rows may be stored bottom-up (the usual) or top-down (negative height).
fn bmp(data: &[u8]) -> Result<Decoded, DecodeError> {
    let bad = || DecodeError::Unsupported("malformed bmp".into());
    let pixel_offset = le_u32(data, 10).ok_or_else(bad)? as usize;
    let header_size = le_u32(data, 14).ok_or_else(bad)? as usize;
    if header_size < 40 {
        return Err(DecodeError::Unsupported("bmp core header".into()));
    }
    let w = le_u32(data, 18).ok_or_else(bad)? as i32;
    let raw_h = le_u32(data, 22).ok_or_else(bad)? as i32;
    let planes = le_u16(data, 26).ok_or_else(bad)?;
    let bpp = le_u16(data, 28).ok_or_else(bad)? as u32;
    let compression = le_u32(data, 30).ok_or_else(bad)?;
    if planes != 1 || compression != 0 {
        return Err(DecodeError::Unsupported("compressed bmp".into()));
    }
    if w <= 0 || raw_h == 0 || raw_h == i32::MIN {
        return Err(DecodeError::Unsupported("invalid bmp dimensions".into()));
    }
    let top_down = raw_h < 0;
    let (w, h) = (w as u32, raw_h.unsigned_abs());
    check_pixels(w as u64, h as u64)?;
    if !matches!(bpp, 1 | 4 | 8 | 24 | 32) {
        return Err(DecodeError::Unsupported(format!(
            "bmp with {bpp} bits per pixel"
        )));
    }
    // Palette: BGRX entries right after the header.
    let colors = if bpp <= 8 {
        let n = le_u32(data, 46).ok_or_else(bad)? as usize;
        if n == 0 {
            1 << bpp
        } else {
            n.min(1 << bpp)
        }
    } else {
        0
    };
    let palette_at = 14 + header_size;
    let palette = data
        .get(palette_at..palette_at + colors * 4)
        .ok_or_else(bad)?;
    let stride = ((w as u64 * bpp as u64).div_ceil(32) * 4) as usize;
    let needed = pixel_offset as u64 + stride as u64 * h as u64;
    if needed > data.len() as u64 {
        return Err(bad());
    }
    let mut rgb = Vec::with_capacity(w as usize * h as usize * 3);
    for y in 0..h as usize {
        let src_row = if top_down { y } else { h as usize - 1 - y };
        let row = &data[pixel_offset + src_row * stride..][..stride];
        for x in 0..w as usize {
            let px: [u8; 3] = match bpp {
                24 | 32 => {
                    let o = x * (bpp as usize / 8);
                    [row[o + 2], row[o + 1], row[o]]
                }
                _ => {
                    let per_byte = 8 / bpp as usize;
                    let byte = row[x / per_byte];
                    let shift = 8 - bpp as usize * (x % per_byte + 1);
                    let idx = ((byte >> shift) & ((1u16 << bpp) - 1) as u8) as usize;
                    let e = palette.get(idx * 4..idx * 4 + 3).unwrap_or(&[0, 0, 0]);
                    [e[2], e[1], e[0]]
                }
            };
            rgb.extend_from_slice(&px);
        }
    }
    Ok(Decoded {
        format: Format::Bmp,
        src_width: w,
        src_height: h,
        width: w,
        height: h,
        rgb,
        orientation: 1,
    })
}

#[cfg(test)]
mod gif_bomb_tests {
    use super::*;

    /// A GIF whose first frame claims 65535x65535 pixels but carries almost no data.
    fn gif_with_huge_frame() -> Vec<u8> {
        let mut g = b"GIF89a".to_vec();
        g.extend_from_slice(&[1, 0, 1, 0]); // logical screen 1x1
        g.extend_from_slice(&[0x80, 0, 0]); // global colour table of 2 entries
        g.extend_from_slice(&[0, 0, 0, 255, 255, 255]);
        g.push(0x2c); // image descriptor
        g.extend_from_slice(&[0, 0, 0, 0, 0xff, 0xff, 0xff, 0xff, 0]); // 65535 x 65535, no local table
        g.push(2); // LZW minimum code size
        g.extend_from_slice(&[2, 0x44, 0x01, 0]); // one tiny data block, then the terminator
        g.push(0x3b);
        g
    }

    #[test]
    fn a_frame_larger_than_the_pixel_budget_is_refused_not_allocated() {
        // TooLarge, not Unsupported: the agent sends "unsupported" input to its in-process decoder.
        assert!(matches!(
            decode(&gif_with_huge_frame(), 96),
            Err(DecodeError::TooLarge(_))
        ));
    }
}
