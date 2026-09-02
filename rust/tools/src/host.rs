// Helpers that every binary of the tools crate shares.
// The drivers and the fuzz entry point embed the same locale blob and decode the same hex tokens.
// Not every binary uses every helper, so the dead code lint stays off here.

#![allow(dead_code)]

use crate::engine;
use crate::vg;

pub static DATA: &[u8] = include_bytes!("../../src/data.bin");

// load_base validates the embedded blob and returns the base locale.
pub fn load_base() -> engine::Locale {
    let (base, ok) = engine::LocaleLoad(vg::lit(DATA));
    assert!(ok, "embedded locale data failed to load");
    base
}

// unhex decodes one protocol token into a string allocated in mem.
// The token "-" stands for the empty string.
pub fn unhex(mem: &vg::Arena, tok: &str) -> vg::Str {
    if tok == "-" {
        return vg::zero();
    }
    let t = tok.as_bytes();
    assert!(t.len().is_multiple_of(2), "bad hex token");
    let s = vg::make::<u8>(mem, (t.len() / 2) as i64);
    for i in 0..t.len() / 2 {
        let hi = hexval(t[2 * i]);
        let lo = hexval(t[2 * i + 1]);
        unsafe { *s.p.add(i) = hi << 4 | lo };
    }
    vg::Str { p: s.p, len: s.len }
}

pub fn hexval(c: u8) -> u8 {
    match c {
        b'0'..=b'9' => c - b'0',
        b'a'..=b'f' => c - b'a' + 10,
        b'A'..=b'F' => c - b'A' + 10,
        _ => panic!("bad hex digit"),
    }
}
