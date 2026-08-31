// Differential driver for the Rust instantiation of the revera engine.
// It reads protocol commands on stdin, one per line, and prints one output line per command.
// go1/revera/driver_host.go, the Go reference implementation, defines the protocol.
//
// The host owns three arenas.
// Locale data lives in the persistent arena.
// A compiled pattern lives in the pattern arena until the next compile.
// Everything one operation allocates comes from the scratch arena, which resets before each operation.
// Each engine call receives the arena that must back its allocations.

#![allow(non_snake_case)]

#[allow(clippy::all)]
mod engine;
mod host;
mod vg;

use std::io::{BufRead, Write};

fn hex_out(s: vg::Str) -> String {
    if s.len == 0 {
        return "-".to_string();
    }
    let mut out = String::with_capacity(2 * s.len as usize);
    for b in s.bytes() {
        out.push_str(&format!("{:02x}", b));
    }
    out
}

fn main() {
    let stdin = std::io::stdin();
    let stdout = std::io::stdout();
    let mut w = std::io::BufWriter::new(stdout.lock());

    let persistent = vg::Arena::new();
    let pattern = vg::Arena::new();
    let scratch = vg::Arena::new();

    let mut base = host::load_base();
    let mut cur = engine::LocalePOSIX();
    let mut re: engine::Regexp = vg::zero();
    let mut valid = false;

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
            "C" => {
                let flags: u32 = f[1].parse().unwrap();
                valid = false;
                re = vg::zero();
                pattern.reset();
                scratch.reset();
                let pat = host::unhex(&pattern, f[2]);
                let (compiled, err) = engine::Compile(&pattern, pat, cur, flags);
                if err.Code != 0 {
                    writeln!(w, "C {} {} 0", err.Code, err.Pos).unwrap();
                } else {
                    re = compiled;
                    valid = true;
                    writeln!(w, "C 0 0 {}", engine::NumSub(&mut re)).unwrap();
                }
            }
            "X" => {
                if !valid {
                    writeln!(w, "X ERR").unwrap();
                } else {
                    scratch.reset();
                    let eflags: u32 = f[1].parse().unwrap();
                    let subject = host::unhex(&scratch, f[2]);
                    let n = engine::NumSub(&mut re);
                    let pmatch = vg::make::<engine::Match>(&scratch, n + 1);
                    let (matched, err) = engine::Exec(&scratch, &mut re, subject, pmatch, eflags);
                    if err.Code != 0 {
                        writeln!(w, "X {} 0", err.Code).unwrap();
                    } else if !matched {
                        writeln!(w, "X 0 0").unwrap();
                    } else {
                        write!(w, "X 0 1").unwrap();
                        for i in 0..pmatch.len {
                            let m = pmatch.get(i);
                            write!(w, " {},{}", m.So, m.Eo).unwrap();
                        }
                        writeln!(w).unwrap();
                    }
                }
            }
            "R" => {
                if !valid {
                    writeln!(w, "R ERR").unwrap();
                } else {
                    scratch.reset();
                    let limit: i64 = f[1].parse().unwrap();
                    let eflags: u32 = f[2].parse().unwrap();
                    let repl = host::unhex(&scratch, f[3]);
                    let subject = host::unhex(&scratch, f[4]);
                    let (out, err) =
                        engine::ReplaceAll(&scratch, &mut re, subject, repl, limit, eflags);
                    if err.Code != 0 {
                        writeln!(w, "R {} {} -", err.Code, err.Pos).unwrap();
                    } else {
                        writeln!(w, "R 0 0 {}", hex_out(out)).unwrap();
                    }
                }
            }
            "I" => {
                if !valid {
                    writeln!(w, "I ERR").unwrap();
                } else {
                    scratch.reset();
                    let limit: i64 = f[1].parse().unwrap();
                    let eflags: u32 = f[2].parse().unwrap();
                    let subject = host::unhex(&scratch, f[3]);
                    let (mut iter, ierr) = engine::MatchIterInit(&mut re, limit);
                    if ierr.Code != 0 {
                        writeln!(w, "I {} 0", ierr.Code).unwrap();
                    } else {
                        let n = engine::NumSub(&mut re);
                        let pmatch = vg::make::<engine::Match>(&scratch, n + 1);
                        let mut rows = String::new();
                        let mut count: i64 = 0;
                        let mut failed = 0i32;
                        loop {
                            let (got, err) = engine::MatchIterNext(
                                &scratch, &mut re, &mut iter, subject, eflags, pmatch,
                            );
                            if err.Code != 0 {
                                failed = err.Code;
                                break;
                            }
                            if !got {
                                break;
                            }
                            if count > 0 {
                                rows.push('|');
                            }
                            for i in 0..pmatch.len {
                                if i > 0 {
                                    rows.push(',');
                                }
                                let m = pmatch.get(i);
                                rows.push_str(&format!("{},{}", m.So, m.Eo));
                            }
                            count += 1;
                        }
                        if failed != 0 {
                            writeln!(w, "I {} 0", failed).unwrap();
                        } else if count > 0 {
                            writeln!(w, "I 0 {} {}", count, rows).unwrap();
                        } else {
                            writeln!(w, "I 0 0").unwrap();
                        }
                    }
                }
            }
            "T" => {
                if !valid {
                    writeln!(w, "T ERR").unwrap();
                } else {
                    scratch.reset();
                    let max_input: i64 = f[1].parse().unwrap();
                    let mut c = engine::ContractFor(&mut re, max_input);
                    writeln!(
                        w,
                        "T {} {} {} {}",
                        c.HasSolver as i32,
                        engine::ContractHeapBytes(&mut c),
                        engine::ContractStackBytes(&mut c),
                        engine::ContractSteps(&mut c)
                    )
                    .unwrap();
                }
            }
            "O" => {
                let lo: i64 = f[1].parse().unwrap();
                let hi: i64 = f[2].parse().unwrap();
                let mut h: u64 = 0xcbf29ce484222325;
                let hi32 = hi as i32;
                let mut r = lo as i32;
                while r < hi32 {
                    h ^= engine::localeToUpper(&mut cur, r) as u32 as u64;
                    h = h.wrapping_mul(0x100000001b3);
                    h ^= engine::localeToLower(&mut cur, r) as u32 as u64;
                    h = h.wrapping_mul(0x100000001b3);
                    r = r.wrapping_add(1);
                }
                writeln!(w, "O {}", h).unwrap();
            }
            _ => panic!("unknown driver command"),
        }
        w.flush().unwrap();
    }
}
