// Fuzz entry point for the TypeScript instantiation of the revera engine.
// dev/internal/protocol/fuzz.go, the Go reference, defines the input format that every target shares.
//
// The property is freedom from exceptions.
// Every result is ignored.
//
// Input format:
//
//   byte 0        compile flags, masked with 0x0f
//   byte 1        bits 0 and 1 are the exec flags, bit 4 selects locale "cs", else bit 5 selects locale "tr"
//   byte 2        n, the pattern length
//   bytes 3..     the pattern, n bytes or fewer if the input ends early
//   next byte     m, the replacement length
//   next m bytes  the replacement, fewer if the input ends early
//   rest          the subject

import * as vg from "./vg.ts";
import * as engine from "./engine.ts";

const CS = vg.lit("cs");
const TR = vg.lit("tr");

export function fuzzOne(base: engine.Locale, data: Uint8Array): void {
    if (data.length < 3) {
        return;
    }
    const cflags = data[0] & 0x0f;
    const eflags = data[1] & 0x03;
    let pos = 3;
    const take = (n: number): vg.Str => {
        if (n > data.length - pos) {
            n = data.length - pos;
        }
        const s = data.subarray(pos, pos + n);
        pos += n;
        return s;
    };
    const pattern = take(data[2]);
    let replacement = vg.EMPTY;
    if (pos < data.length) {
        const m = data[pos++];
        replacement = take(m);
    }
    const subject = take(data.length - pos);

    let loc = engine.LocalePOSIX();
    if (data[1] & 0x30) {
        const sel = engine.LocaleSelect(base, data[1] & 0x10 ? CS : TR, vg.EMPTY);
        if (!sel[1]) {
            return;
        }
        loc = sel[0];
    }
    const compiled = engine.Compile(pattern, loc, cflags);
    if (compiled[1].Code !== 0) {
        return;
    }
    const re = compiled[0];
    const pmatch = vg.make(engine.Match.elem, engine.NumSub(re) + 1);
    engine.Exec(re, subject, pmatch, eflags);
    engine.ReplaceAll(re, subject, replacement, -1, eflags);
    const init = engine.MatchIterInit(re, 3);
    if (init[1].Code === 0) {
        const iter = init[0];
        for (;;) {
            const next = engine.MatchIterNext(re, iter, subject, eflags, pmatch);
            if (next[1].Code !== 0 || !next[0]) {
                break;
            }
        }
    }
    const c = engine.ContractFor(re, subject.length);
    engine.ContractHeapBytes(c);
    engine.ContractStackBytes(c);
    engine.ContractSteps(c);
}
