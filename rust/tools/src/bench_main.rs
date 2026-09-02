// Bench driver for the Rust instantiation of the revera engine.
// It reads bench protocol commands on stdin, one per line, and prints one answer line per command.
// dev/internal/protocol/bench.go, the Go reference implementation, defines the protocol.
//
// The host owns four arenas.
// Locale data lives in the persistent arena.
// The decoded strings of one B command live in the input arena, which resets at the start of each B command.
// A compiled pattern lives in the pattern arena, which resets at each compile.
// Everything one operation allocates comes from the scratch arena, which resets before each operation.
//
// A B command runs one untimed pass that counts arena requests, then the timed passes.
// The counts come from the arena counters, so they reflect engine-level requests and nothing else.

#![allow(non_snake_case)]

#[path = "../../src/engine.rs"]
#[allow(clippy::all)]
mod engine;
mod host;
#[path = "../../src/vg.rs"]
mod vg;

use std::io::{BufRead, Write};
use std::time::Instant;

// requests sums the counters of the two arenas an operation can touch.
fn requests(pattern: &vg::Arena, scratch: &vg::Arena) -> (u64, u64) {
    let (pa, pb) = pattern.stats();
    let (sa, sb) = scratch.stats();
    (pa + sa, pb + sb)
}

// pass runs the operation iters times.
// It is generic over the closure type, so every bench kind gets its own copy with the call inlined.
fn pass<F: FnMut()>(op: &mut F, iters: i64) {
    for _ in 0..iters {
        op();
    }
}

// measure answers one B command for the operation op.
// The first pass is untimed and counts arena requests, then each repetition is timed.
fn measure<W: Write, F: FnMut()>(
    w: &mut W,
    name: &str,
    iters: i64,
    reps: i64,
    pattern: &vg::Arena,
    scratch: &vg::Arena,
    mut op: F,
) {
    let (allocs0, bytes0) = requests(pattern, scratch);
    pass(&mut op, iters);
    let (allocs1, bytes1) = requests(pattern, scratch);
    write!(
        w,
        "B {} 0 {} {}",
        name,
        (bytes1 - bytes0) / iters as u64,
        (allocs1 - allocs0) / iters as u64
    )
    .unwrap();

    for _ in 0..reps {
        let start = Instant::now();
        pass(&mut op, iters);
        write!(w, " {}", start.elapsed().as_nanos()).unwrap();
    }
    writeln!(w).unwrap();
}

fn main() {
    let stdin = std::io::stdin();
    let stdout = std::io::stdout();
    let mut w = std::io::BufWriter::new(stdout.lock());

    let persistent = vg::Arena::new();
    let input = vg::Arena::new();
    let pattern = vg::Arena::new();
    let scratch = vg::Arena::new();

    let mut base = host::load_base();
    let mut cur = engine::LocalePOSIX();

    for line in stdin.lock().lines() {
        let line = line.expect("read error");
        if line.is_empty() {
            continue;
        }
        let f: Vec<&str> = line.split_whitespace().collect();
        match f[0] {
            "P" => {
                cur = engine::LocalePOSIX();
                writeln!(w, "P 1").unwrap();
            }
            "L" => {
                let name = host::unhex(&persistent, f[1]);
                let coll = host::unhex(&persistent, f[2]);
                let (loc, ok) = engine::LocaleSelect(&persistent, &mut base, name, coll);
                if ok {
                    cur = loc;
                }
                writeln!(w, "L {}", ok as i32).unwrap();
            }
            "B" => {
                let name = f[1];
                let kind = f[2];
                let iters: i64 = f[3].parse().unwrap();
                let reps: i64 = f[4].parse().unwrap();
                let flags: u32 = f[5].parse().unwrap();
                assert!(iters > 0, "bench iters must be positive");
                input.reset();
                let pat = host::unhex(&input, f[6]);
                let subject = host::unhex(&input, f[7]);
                let repl = host::unhex(&input, f[8]);

                pattern.reset();
                scratch.reset();
                let (mut re, err) = engine::Compile(&pattern, pat, cur, flags);
                if err.Code != 0 {
                    writeln!(w, "B {} {} 0 0", name, err.Code).unwrap();
                    w.flush().unwrap();
                    continue;
                }
                let nsub = engine::NumSub(&mut re);

                match kind {
                    "compile" => measure(&mut w, name, iters, reps, &pattern, &scratch, || {
                        pattern.reset();
                        engine::Compile(&pattern, pat, cur, flags);
                    }),
                    "match" => measure(&mut w, name, iters, reps, &pattern, &scratch, || {
                        scratch.reset();
                        let pmatch = vg::make::<engine::Match>(&scratch, nsub + 1);
                        engine::Exec(&scratch, &mut re, subject, pmatch, 0);
                    }),
                    "replace" => measure(&mut w, name, iters, reps, &pattern, &scratch, || {
                        scratch.reset();
                        engine::ReplaceAll(&scratch, &mut re, subject, repl, -1, 0);
                    }),
                    _ => panic!("unknown bench kind"),
                }
            }
            _ => panic!("unknown bench command"),
        }
        w.flush().unwrap();
    }
}
