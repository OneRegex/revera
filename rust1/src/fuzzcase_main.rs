// Seed pack runner for the Rust fuzz entry point.
// fuzzcase <packfile> replays every record of the pack through fuzz_one.
// A record is a 4-byte little-endian length followed by that many bytes.
// A crash or an assert failure is the only signal.
// A missing or truncated pack is an error.

#[allow(clippy::all)]
mod engine;
mod fuzz;
mod host;
mod vg;

use std::process::ExitCode;

fn main() -> ExitCode {
    let args: Vec<String> = std::env::args().collect();
    if args.len() != 2 {
        eprintln!("usage: fuzzcase <packfile>");
        return ExitCode::from(1);
    }
    let pack = match std::fs::read(&args[1]) {
        Ok(pack) => pack,
        Err(err) => {
            eprintln!("fuzzcase: {}: {}", args[1], err);
            return ExitCode::from(1);
        }
    };

    let mut count: u64 = 0;
    let mut pos = 0usize;
    while pos < pack.len() {
        if pack.len() - pos < 4 {
            eprintln!(
                "fuzzcase: {}: truncated record header at offset {}",
                args[1], pos
            );
            return ExitCode::from(1);
        }
        let n =
            u32::from_le_bytes([pack[pos], pack[pos + 1], pack[pos + 2], pack[pos + 3]]) as usize;
        pos += 4;
        if pack.len() - pos < n {
            eprintln!(
                "fuzzcase: {}: truncated record at offset {}",
                args[1],
                pos - 4
            );
            return ExitCode::from(1);
        }
        fuzz::fuzz_one(&pack[pos..pos + n]);
        pos += n;
        count += 1;
    }
    println!("fuzzcase: {} inputs", count);
    ExitCode::SUCCESS
}
