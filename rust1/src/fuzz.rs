// Fuzz entry point for the Rust instantiation of the revera engine.
// fuzz_one decodes one input into flags, a locale choice, a pattern, a replacement and a subject.
// It then runs every engine operation on them and ignores the results.
// Crash freedom is the property under test.
// The Go, Zig and C++ targets read the same layout, so a corpus transfers between them.
//
//     byte 0        compile flags, masked to the four defined bits
//     byte 1        bits 0 and 1 are the exec flags; bit 4 selects locale "cs", else bit 5 selects locale "tr"
//     byte 2        pattern length n
//     bytes 3..     pattern, n bytes, fewer if the input ends early
//     next byte     replacement length m
//     next m bytes  replacement, fewer if the input ends early
//     rest          subject
//
// One arena backs everything one input needs, and it resets after every input.
// The base locale loads once per thread, because LocaleLoad validates the whole blob.

use crate::engine;
use crate::host;
use crate::vg;
use std::cell::RefCell;

struct Session {
    mem: vg::Arena,
    base: engine::Locale,
}

thread_local! {
    static SESSION: RefCell<Session> = RefCell::new(Session::new());
}

impl Session {
    fn new() -> Session {
        Session {
            mem: vg::Arena::new(),
            base: host::load_base(),
        }
    }

    fn run(&mut self, data: &[u8]) {
        if data.len() < 3 {
            return;
        }
        let cflags = (data[0] & 0x0f) as u32;
        let eflags = (data[1] & 0x03) as u32;
        let n = data[2] as usize;
        let pat_end = (3 + n).min(data.len());
        let pattern = &data[3..pat_end];
        let (replacement, subject) = if pat_end < data.len() {
            let m = data[pat_end] as usize;
            let repl_end = (pat_end + 1 + m).min(data.len());
            (&data[pat_end + 1..repl_end], &data[repl_end..])
        } else {
            (&data[..0], &data[..0])
        };

        let loc = if data[1] & 0x30 != 0 {
            let name: &'static [u8] = if data[1] & 0x10 != 0 { b"cs" } else { b"tr" };
            let (loc, ok) =
                engine::LocaleSelect(&self.mem, &mut self.base, vg::lit(name), vg::zero());
            if !ok {
                return;
            }
            loc
        } else {
            engine::LocalePOSIX()
        };

        let (mut re, err) = engine::Compile(&self.mem, vg::view(pattern), loc, cflags);
        if err.Code != 0 {
            return;
        }
        let subject = vg::view(subject);
        let pmatch = vg::make::<engine::Match>(&self.mem, engine::NumSub(&mut re) + 1);
        engine::Exec(&self.mem, &mut re, subject, pmatch, eflags);
        engine::ReplaceAll(
            &self.mem,
            &mut re,
            subject,
            vg::view(replacement),
            -1,
            eflags,
        );

        let (mut iter, ierr) = engine::MatchIterInit(&mut re, 3);
        if ierr.Code == 0 {
            loop {
                let (got, err) =
                    engine::MatchIterNext(&self.mem, &mut re, &mut iter, subject, eflags, pmatch);
                if err.Code != 0 || !got {
                    break;
                }
            }
        }

        let mut c = engine::ContractFor(&mut re, subject.len);
        engine::ContractHeapBytes(&mut c);
        engine::ContractStackBytes(&mut c);
        engine::ContractSteps(&mut c);
    }
}

pub fn fuzz_one(data: &[u8]) {
    SESSION.with(|cell| {
        let mut s = cell.borrow_mut();
        s.run(data);
        s.mem.reset();
    });
}
