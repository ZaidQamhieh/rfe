use rfe_thumbd::decode::{self, DecodeError};
use rfe_thumbd::render::{self, RenderError};

fn data(name: &str) -> Vec<u8> {
    std::fs::read(format!("{}/tests/data/{name}", env!("CARGO_MANIFEST_DIR"))).unwrap()
}

/// Decodes a thumbnail back to RGB for inspection.
fn pixels(jpeg: &[u8]) -> (usize, usize, Vec<u8>) {
    let mut d = jpeg_decoder::Decoder::new(jpeg);
    let px = d.decode().unwrap();
    let i = d.info().unwrap();
    assert_eq!(i.pixel_format, jpeg_decoder::PixelFormat::RGB24);
    (i.width as usize, i.height as usize, px)
}

fn at(img: &(usize, usize, Vec<u8>), x: usize, y: usize) -> [u8; 3] {
    let o = (y * img.0 + x) * 3;
    [img.2[o], img.2[o + 1], img.2[o + 2]]
}

fn is_red(p: [u8; 3]) -> bool {
    p[0] > 190 && p[1] < 90 && p[2] < 90
}

fn near(a: [u8; 3], b: [u8; 3], tol: i32) -> bool {
    a.iter()
        .zip(b)
        .all(|(x, y)| (*x as i32 - y as i32).abs() <= tol)
}

#[test]
fn a_landscape_photo_is_fitted_and_never_upscaled() {
    let t = render::render(&data("photo.jpg"), 128).unwrap();
    assert_eq!(
        (t.width, t.height, t.src_width, t.src_height),
        (128, 96, 400, 300)
    );
    assert_eq!(t.format, "jpeg");
    let t = render::render(&data("photo.jpg"), 1024).unwrap();
    assert_eq!(
        (t.width, t.height),
        (400, 300),
        "smaller than the box stays as it is"
    );
    let img = pixels(&render::render(&data("photo.jpg"), 128).unwrap().jpeg);
    assert!(is_red(at(&img, 40, 30)), "the red block is top left");
}

#[test]
fn exif_orientation_is_applied_before_fitting() {
    let t = render::render(&data("orient6.jpg"), 128).unwrap();
    assert_eq!((t.width, t.height), (96, 128), "rotated to portrait");
    let img = pixels(&t.jpeg);
    // Rotated 90 degrees clockwise, the block that was top left is top right.
    assert!(is_red(at(&img, 70, 20)), "{:?}", at(&img, 70, 20));
    assert!(!is_red(at(&img, 20, 20)));

    let t = render::render(&data("orient3.jpg"), 128).unwrap();
    assert_eq!((t.width, t.height), (128, 96));
    let img = pixels(&t.jpeg);
    assert!(
        is_red(at(&img, 128 - 40, 96 - 30)),
        "rotated 180: bottom right"
    );
}

#[test]
fn every_decoder_produces_a_thumbnail() {
    for (name, w, h) in [
        ("gray.jpg", 128, 96),
        ("cmyk.jpg", 128, 96),
        ("progressive.jpg", 128, 96),
        ("palette.png", 128, 96),
        ("gray16.png", 64, 64),
        ("alpha.png", 128, 64),
        ("anim.gif", 120, 80),
        ("lossy.webp", 128, 96),
        ("alpha.webp", 128, 64),
        ("photo.tiff", 128, 96),
        ("deflate.tiff", 128, 96),
        ("raw.tiff", 128, 96),
        ("gray.tiff", 128, 96),
        ("rgba.tiff", 128, 96),
        ("photo.bmp", 128, 96),
        ("pal.bmp", 128, 96),
        ("bits32.bmp", 128, 96),
        ("topdown.bmp", 128, 96),
    ] {
        let t = render::render(&data(name), 128).unwrap_or_else(|e| panic!("{name}: {e:?}"));
        assert_eq!((t.width, t.height), (w, h), "{name}");
        let _ = pixels(&t.jpeg);
    }
}

#[test]
fn colours_survive() {
    let cmyk = pixels(&render::render(&data("cmyk.jpg"), 128).unwrap().jpeg);
    assert!(
        is_red(at(&cmyk, 40, 30)),
        "cmyk red block: {:?}",
        at(&cmyk, 40, 30)
    );
    let gray = pixels(&render::render(&data("gray16.png"), 64).unwrap().jpeg);
    assert!(
        near(at(&gray, 10, 10), [156, 156, 156], 4),
        "{:?}",
        at(&gray, 10, 10)
    );
    // Transparency is flattened onto black: transparent on the left, opaque orange on the right.
    let alpha = pixels(&render::render(&data("alpha.png"), 200).unwrap().jpeg);
    assert!(
        near(at(&alpha, 2, 50), [0, 0, 0], 12),
        "{:?}",
        at(&alpha, 2, 50)
    );
    assert!(
        near(at(&alpha, 197, 50), [255, 128, 0], 24),
        "{:?}",
        at(&alpha, 197, 50)
    );
}

#[test]
fn big_jpegs_are_decoded_at_a_reduced_size() {
    let (w, h) = (4000usize, 3000usize);
    let mut rgb = vec![0u8; w * h * 3];
    for y in 0..h {
        for x in 0..w {
            let o = (y * w + x) * 3;
            rgb[o] = (x * 255 / w) as u8;
            rgb[o + 1] = (y * 255 / h) as u8;
            rgb[o + 2] = 90;
        }
    }
    let mut jpeg = Vec::new();
    jpeg_encoder::Encoder::new(&mut jpeg, 85)
        .encode(&rgb, w as u16, h as u16, jpeg_encoder::ColorType::Rgb)
        .unwrap();
    let d = decode::decode(&jpeg, 256).unwrap();
    assert_eq!((d.src_width, d.src_height), (4000, 3000));
    assert!(
        d.width < d.src_width && d.width >= 512,
        "DCT scaled: {}x{}",
        d.width,
        d.height
    );
    let t = render::render(&jpeg, 256).unwrap();
    assert_eq!((t.width, t.height), (256, 192));
    let img = pixels(&t.jpeg);
    assert!(
        near(at(&img, 128, 96), [127, 127, 90], 8),
        "{:?}",
        at(&img, 128, 96)
    );
}

#[test]
fn bad_input_is_refused_not_crashed_on() {
    assert!(matches!(
        render::render(&data("notimage.bin"), 128),
        Err(RenderError::Decode(DecodeError::Unsupported(_)))
    ));
    assert!(matches!(
        render::render(b"", 128),
        Err(RenderError::Decode(DecodeError::Unsupported(_)))
    ));
    // A truncated JPEG either decodes what is there or is refused; it never panics.
    let _ = render::render(&data("truncated.jpg"), 128);
    for cut in [10usize, 100, 1000] {
        let b = data("lossy.webp");
        let _ = render::render(&b[..cut.min(b.len())], 128);
        let p = data("palette.png");
        let _ = render::render(&p[..cut.min(p.len())], 128);
    }
}

#[test]
fn a_decompression_bomb_is_refused_before_decoding() {
    // A 10000 x 10000 one-bit PNG is a few kilobytes on disk and 100 megapixels once decoded.
    let mut png = Vec::new();
    {
        let mut enc = png::Encoder::new(&mut png, 10_000, 10_000);
        enc.set_color(png::ColorType::Grayscale);
        enc.set_depth(png::BitDepth::One);
        let mut w = enc.write_header().unwrap();
        w.write_image_data(&vec![0u8; 1250 * 10_000]).unwrap();
    }
    assert!(png.len() < 1 << 20, "{} bytes", png.len());
    assert!(matches!(
        render::render(&png, 128),
        Err(RenderError::Decode(DecodeError::TooLarge(_)))
    ));
}

#[test]
fn random_mutations_never_panic() {
    let mut seed = 12345u32;
    let mut rnd = || {
        seed = seed.wrapping_mul(1664525).wrapping_add(1013904223);
        seed
    };
    for name in [
        "photo.jpg",
        "palette.png",
        "anim.gif",
        "lossy.webp",
        "alpha.webp",
        "orient6.jpg",
    ] {
        let original = data(name);
        for _ in 0..60 {
            let mut b = original.clone();
            for _ in 0..(1 + rnd() % 8) {
                let i = (rnd() as usize) % b.len();
                b[i] = rnd() as u8;
            }
            let r = std::panic::catch_unwind(|| render::render(&b, 64));
            assert!(r.is_ok(), "{name} panicked on a mutated input");
        }
    }
}

#[test]
fn tiff_and_bmp_keep_their_orientation_and_colors() {
    // The same picture in every variant: a red block top left on blue, 200x150, shown at 128x96.
    for name in [
        "photo.tiff",
        "deflate.tiff",
        "raw.tiff",
        "rgba.tiff",
        "photo.bmp",
        "bits32.bmp",
        "topdown.bmp",
    ] {
        let t = render::render(&data(name), 128).unwrap();
        assert_eq!((t.src_width, t.src_height), (200, 150), "{name}");
        let img = pixels(&t.jpeg);
        assert!(
            is_red(at(&img, 10, 8)),
            "{name}: red block top left, got {:?}",
            at(&img, 10, 8)
        );
        assert!(
            near(at(&img, 100, 70), [30, 60, 200], 24),
            "{name}: blue background, got {:?}",
            at(&img, 100, 70)
        );
    }
    // A paletted bitmap is quantized to 16 colors, so only the layout is checked.
    let img = pixels(&render::render(&data("pal.bmp"), 128).unwrap().jpeg);
    assert!(is_red(at(&img, 10, 8)), "{:?}", at(&img, 10, 8));
    let gray = pixels(&render::render(&data("gray.tiff"), 128).unwrap().jpeg);
    let p = at(&gray, 100, 70);
    assert!(
        p[0].abs_diff(p[1]) < 6 && p[1].abs_diff(p[2]) < 6,
        "gray tiff is gray: {p:?}"
    );
}

#[test]
fn damaged_tiff_and_bmp_are_unsupported_not_panics() {
    let tiff = data("photo.tiff");
    let bmp = data("photo.bmp");
    for (name, bytes) in [
        ("tiff cut short", tiff[..tiff.len() / 2].to_vec()),
        ("tiff header only", tiff[..8].to_vec()),
        ("bmp cut short", bmp[..bmp.len() / 2].to_vec()),
        ("bmp header only", bmp[..30].to_vec()),
        ("bm text", b"BM is not an image".to_vec()),
    ] {
        match decode::decode(&bytes, 128) {
            Err(DecodeError::Unsupported(_)) | Err(DecodeError::TooLarge(_)) => {}
            other => panic!("{name}: {:?}", other.map(|d| d.format)),
        }
    }
    // A bitmap that declares a huge size is refused before any pixel memory is allocated.
    let mut huge = bmp.clone();
    huge[18..22].copy_from_slice(&60000u32.to_le_bytes());
    huge[22..26].copy_from_slice(&60000u32.to_le_bytes());
    assert!(matches!(
        decode::decode(&huge, 128),
        Err(DecodeError::TooLarge(_))
    ));
}
