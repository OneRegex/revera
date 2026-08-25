// Differential driver for the Rust instantiation of the revera
// engine. It reads protocol commands on stdin, one per line, and
// prints one output line per command. The protocol is defined by
// go1/revera/driver_host.go, the Go reference implementation.

#![allow(non_snake_case)]

mod engine;
mod vg;

use std::io::{BufRead, Write};

static DATA: &[u8] = include_bytes!("data.bin");

fn unhex(tok: &str) -> vg::Str {
    if tok == "-" {
        return vg::zero();
    }
    let t = tok.as_bytes();
    assert!(t.len() % 2 == 0, "bad hex token");
    let s = vg::make::<u8>((t.len() / 2) as i64);
    for i in 0..t.len() / 2 {
        let hi = hexval(t[2 * i]);
        let lo = hexval(t[2 * i + 1]);
        unsafe { *s.p.add(i) = hi << 4 | lo };
    }
    vg::Str { p: s.p, len: s.len }
}

fn hexval(c: u8) -> u8 {
    match c {
        b'0'..=b'9' => c - b'0',
        b'a'..=b'f' => c - b'a' + 10,
        b'A'..=b'F' => c - b'A' + 10,
        _ => panic!("bad hex digit"),
    }
}

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

    vg::use_persistent_arena();
    let (mut base, ok) = engine::LocaleLoad(vg::lit(DATA));
    assert!(ok, "embedded locale data failed to load");
    let mut cur = engine::LocalePOSIX();
    let mut re: engine::Regexp = vg::zero();
    let mut valid = false;
    vg::use_scratch_arena();

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
                vg::use_persistent_arena();
                let name = unhex(f[1]);
                let coll = unhex(f[2]);
                let (loc, ok) = engine::LocaleSelect(&mut base, name, coll);
                vg::use_scratch_arena();
                if ok {
                    cur = loc;
                }
                writeln!(w, "L {}", ok as i32).unwrap();
            }
            "C" => {
                let flags: u32 = f[1].parse().unwrap();
                valid = false;
                re = vg::zero();
                vg::reset_pattern_arena();
                vg::use_pattern_arena();
                let pat = unhex(f[2]);
                let (compiled, err) = engine::Compile(pat, cur, flags);
                vg::use_scratch_arena();
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
                    vg::reset_scratch_arena();
                    let eflags: u32 = f[1].parse().unwrap();
                    let subject = unhex(f[2]);
                    let n = engine::NumSub(&mut re);
                    let pmatch = vg::make::<engine::Match>(n + 1);
                    let (matched, err) = engine::Exec(&mut re, subject, pmatch, eflags);
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
                    vg::reset_scratch_arena();
                    let limit: i64 = f[1].parse().unwrap();
                    let eflags: u32 = f[2].parse().unwrap();
                    let repl = unhex(f[3]);
                    let subject = unhex(f[4]);
                    let (out, err) = engine::ReplaceAll(&mut re, subject, repl, limit, eflags);
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
                    vg::reset_scratch_arena();
                    let limit: i64 = f[1].parse().unwrap();
                    let eflags: u32 = f[2].parse().unwrap();
                    let subject = unhex(f[3]);
                    let (mut iter, ierr) = engine::MatchIterInit(&mut re, limit);
                    if ierr.Code != 0 {
                        writeln!(w, "I {} 0", ierr.Code).unwrap();
                    } else {
                        let n = engine::NumSub(&mut re);
                        let pmatch = vg::make::<engine::Match>(n + 1);
                        let mut rows = String::new();
                        let mut count: i64 = 0;
                        let mut failed = 0i32;
                        loop {
                            let (got, err) =
                                engine::MatchIterNext(&mut re, &mut iter, subject, eflags, pmatch);
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
                    vg::reset_scratch_arena();
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
