// Probe runner for the Rust instantiation.
// It prints the same lines as dev/internal/conformance/proberef, and dev/internal/conformance/probecheck diffs them.

#![allow(non_snake_case, non_camel_case_types, non_upper_case_globals)]
#![allow(unused_parens, unused_mut, unused_variables, dead_code)]

#[path = "../../src/probe_engine.rs"]
#[allow(clippy::all)]
mod probe_engine;
#[path = "../../src/vg.rs"]
mod vg;

use probe_engine as p;

const MIN_I64: i64 = i64::MIN;

fn mk(a: &'static [u8], b: &'static [u8], n: i32) -> p::Tagged {
    p::Tagged {
        Tags: [vg::lit(a), vg::lit(b)],
        N: n,
    }
}

fn eq(x: p::Tagged, y: p::Tagged) -> i32 {
    if p::TaggedEq(x, y) {
        1
    } else {
        0
    }
}

fn main() {
    let mem = vg::Arena::new();

    for ab in [
        (MIN_I64, -1i64),
        (7, -2),
        (-7, 2),
        (MIN_I64, 1),
        (1, MIN_I64),
    ] {
        let (q, r) = p::DivMod(ab.0, ab.1);
        println!("divmod {} {} = {} {}", ab.0, ab.1, q, r);
    }
    let (q32, r32) = p::DivMod32(i32::MIN, -1);
    println!("divmod32 = {} {}", q32, r32);
    let (q32b, r32b) = p::DivMod32(9, -4);
    println!("divmod32b = {} {}", q32b, r32b);
    println!(
        "bytes = {} {}",
        p::BytesProbe(&mem, vg::lit(b"hello")),
        p::BytesProbe(&mem, vg::zero::<vg::Str>())
    );
    let mut c1 = vg::zero::<p::Counter>();
    println!("range = {}", p::RangeProbe(&mem, &mut c1));
    println!(
        "rangeval = {}",
        p::RangeValProbe(vg::slice_of::<i32>(&mem, &[3, 5, 7]))
    );
    println!("rangeint = {}", p::RangeIntProbe(5));
    println!("partial = {}", p::PartialArray());
    println!(
        "tagged = {} {} {}",
        eq(mk(b"a", b"b", 1), mk(b"a", b"b", 1)),
        eq(mk(b"a", b"b", 1), mk(b"a", b"c", 1)),
        eq(mk(b"a", b"b", 1), mk(b"a", b"b", 2))
    );
    let mut c2 = vg::zero::<p::Counter>();
    println!("orderargs = {}", p::OrderArgs(&mem, &mut c2));
    let mut c3 = vg::zero::<p::Counter>();
    println!("orderbinary = {}", p::OrderBinary(&mem, &mut c3));
    let mut c4 = vg::zero::<p::Counter>();
    println!("orderindex = {}", p::OrderIndex(&mem, &mut c4));
    println!("spare = {}", p::SpareProbe(&mem));
    println!("nil = {}", p::NilProbe(&mem));
    println!(
        "wrap = {} {}",
        p::WrapProbe(MIN_I64, 3),
        p::WrapProbe(7, -9)
    );
    println!(
        "narrow32 = {} {}",
        p::Narrow32(i32::MIN, -1),
        p::Narrow32(-17, 5)
    );
    println!("wrapu8 = {}", p::WrapU8(3, 200));
    println!("andnot = {}", p::AndNotProbe(0xF0F0F0F0, 0xFF00FF00));
    println!("shift = {}", p::ShiftProbe(0x8000000000000001, 7));
    println!("conv = {} {}", p::ConvProbe(-99), p::ConvProbe(300));
    println!("subwrite = {}", p::SubWrite(&mem, 4));
    let mut c5 = vg::zero::<p::Counter>();
    println!("andnotorder = {}", p::AndNotOrder(&mem, &mut c5));
    println!("zeroarray = {}", p::ZeroArray());
    println!("makeu64 = {}", p::MakeU64(&mem, 6));
    let mut c6 = vg::zero::<p::Counter>();
    println!("pickarray = {}", p::PickArray(&mem, &mut c6));
}
