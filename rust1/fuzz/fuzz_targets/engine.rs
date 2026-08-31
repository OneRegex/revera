// libFuzzer target for the Rust instantiation of the revera engine.
// It includes the runtime, the generated engine and the shared entry point by path.
// The target therefore needs no library crate and links no public API.

#![no_main]

#[path = "../../src/vg.rs"]
mod vg;

#[path = "../../src/engine.rs"]
#[allow(clippy::all)]
mod engine;

#[path = "../../src/host.rs"]
mod host;

#[path = "../../src/fuzz.rs"]
mod fuzz;

libfuzzer_sys::fuzz_target!(|data: &[u8]| {
    fuzz::fuzz_one(data);
});
