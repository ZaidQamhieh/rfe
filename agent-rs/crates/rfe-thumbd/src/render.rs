//! Fit, orient and encode: the same output the Go renderer produces (longest side at most `max`, never upscaled,
//! Lanczos filter, JPEG quality 80, EXIF orientation applied).

use crate::decode::{self, DecodeError};
use fast_image_resize::images::Image;
use fast_image_resize::{FilterType, PixelType, ResizeAlg, ResizeOptions, Resizer};

/// JPEG quality of the thumbnails (the Go renderer's setting).
pub const JPEG_QUALITY: u8 = 80;

#[derive(Debug)]
pub enum RenderError {
    Decode(DecodeError),
    Internal(String),
}

impl From<DecodeError> for RenderError {
    fn from(e: DecodeError) -> Self {
        RenderError::Decode(e)
    }
}

pub struct Thumb {
    pub jpeg: Vec<u8>,
    pub width: u32,
    pub height: u32,
    pub src_width: u32,
    pub src_height: u32,
    pub format: &'static str,
}

/// imaging.Fit: no change when the image already fits, otherwise scale the longer side to `max` (truncating).
pub fn fit_dims(w: u32, h: u32, max: u32) -> (u32, u32) {
    if w <= max && h <= max {
        return (w, h);
    }
    let (sw, sh, m) = (w as f64, h as f64, max as f64);
    let (nw, nh) = if sw / sh > 1.0 {
        (m, (m / (sw / sh)).floor())
    } else {
        ((m * (sw / sh)).floor(), m)
    };
    ((nw as u32).max(1), (nh as u32).max(1))
}

/// Where each source pixel lands for an EXIF orientation (1-8), as a function of the output size.
fn orient(rgb: &[u8], w: usize, h: usize, orientation: u8) -> (Vec<u8>, usize, usize) {
    if orientation <= 1 || orientation > 8 {
        return (rgb.to_vec(), w, h);
    }
    let swap = orientation >= 5;
    let (ow, oh) = if swap { (h, w) } else { (w, h) };
    let mut out = vec![0u8; rgb.len()];
    for y in 0..h {
        for x in 0..w {
            let (nx, ny) = match orientation {
                2 => (w - 1 - x, y),
                3 => (w - 1 - x, h - 1 - y),
                4 => (x, h - 1 - y),
                5 => (y, x),
                6 => (h - 1 - y, x),
                7 => (h - 1 - y, w - 1 - x),
                8 => (y, w - 1 - x),
                _ => (x, y),
            };
            let s = (y * w + x) * 3;
            let d = (ny * ow + nx) * 3;
            out[d..d + 3].copy_from_slice(&rgb[s..s + 3]);
        }
    }
    (out, ow, oh)
}

fn resize(rgb: Vec<u8>, w: u32, h: u32, nw: u32, nh: u32) -> Result<Vec<u8>, RenderError> {
    let src = Image::from_vec_u8(w, h, rgb, PixelType::U8x3)
        .map_err(|e| RenderError::Internal(e.to_string()))?;
    let mut dst = Image::new(nw, nh, PixelType::U8x3);
    Resizer::new()
        .resize(
            &src,
            &mut dst,
            &ResizeOptions::new().resize_alg(ResizeAlg::Convolution(FilterType::Lanczos3)),
        )
        .map_err(|e| RenderError::Internal(e.to_string()))?;
    Ok(dst.into_vec())
}

pub fn render(data: &[u8], max: u32) -> Result<Thumb, RenderError> {
    let max = max.max(1);
    // The fit box applies to the oriented image, so size it from the oriented source dimensions.
    let d = decode::decode(data, max)?;
    let swap = d.orientation >= 5 && d.orientation <= 8;
    let (ow, oh) = if swap {
        (d.src_height, d.src_width)
    } else {
        (d.src_width, d.src_height)
    };
    let (fw, fh) = fit_dims(ow, oh, max);
    // Resize in the stored orientation, then rotate the small result.
    let (tw, th) = if swap { (fh, fw) } else { (fw, fh) };
    let rgb = if (d.width, d.height) == (tw, th) {
        d.rgb
    } else {
        resize(d.rgb, d.width, d.height, tw, th)?
    };
    let (rgb, ow, oh) = orient(&rgb, tw as usize, th as usize, d.orientation);

    // The JPEG encoder takes u16 dimensions; a larger output would wrap silently (audit F-86).
    if ow > u16::MAX as usize || oh > u16::MAX as usize {
        return Err(RenderError::Internal(
            "thumbnail dimensions are too large".into(),
        ));
    }
    let mut jpeg = Vec::with_capacity(ow * oh / 4 + 1024);
    let mut enc = jpeg_encoder::Encoder::new(&mut jpeg, JPEG_QUALITY);
    enc.set_sampling_factor(jpeg_encoder::SamplingFactor::F_2_2);
    enc.encode(&rgb, ow as u16, oh as u16, jpeg_encoder::ColorType::Rgb)
        .map_err(|e| RenderError::Internal(e.to_string()))?;
    Ok(Thumb {
        jpeg,
        width: ow as u32,
        height: oh as u32,
        src_width: d.src_width,
        src_height: d.src_height,
        format: d.format.name(),
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn fit_matches_imaging() {
        assert_eq!(fit_dims(100, 50, 256), (100, 50), "never upscaled");
        assert_eq!(fit_dims(4000, 3000, 256), (256, 192));
        assert_eq!(fit_dims(3000, 4000, 256), (192, 256));
        assert_eq!(fit_dims(1000, 1000, 256), (256, 256));
        assert_eq!(
            fit_dims(10000, 10, 256),
            (256, 1),
            "extreme aspect keeps one pixel"
        );
        assert_eq!(fit_dims(1920, 1080, 100), (100, 56), "truncates like Go");
    }

    #[test]
    fn orientation_moves_pixels_like_exif() {
        // 2x1 image: A B
        let img = [1, 1, 1, 2, 2, 2];
        assert_eq!(orient(&img, 2, 1, 2).0, [2, 2, 2, 1, 1, 1], "mirror");
        let (r, w, h) = orient(&img, 2, 1, 6);
        assert_eq!((w, h), (1, 2), "rotate 90 cw swaps");
        assert_eq!(r, [1, 1, 1, 2, 2, 2], "A on top");
        let (r, ..) = orient(&img, 2, 1, 8);
        assert_eq!(r, [2, 2, 2, 1, 1, 1], "rotate 270 cw puts B on top");
        assert_eq!(orient(&img, 2, 1, 3).0, [2, 2, 2, 1, 1, 1]);
    }
}
