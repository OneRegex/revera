// Benchmark host for the TypeScript instantiation of the revera engine.
// It reads bench protocol commands on stdin, one per line, and prints one answer line per command.
// dev/internal/protocol/bench.go, the Go reference implementation, defines the protocol.
//
// The untimed pass reads the allocation counters of the runtime.
// They count the buffers the engine asks the runtime for, and their sizes at the element widths the runtime assumes.
// The timed passes read the monotonic clock around a whole pass.

import * as vg from "./vg.ts";
import * as engine from "./engine.ts";
import { baseLocale, decode, int, lines, need } from "./host.ts";

type Kind = "compile" | "match" | "replace";

interface Case {
    kind: Kind;
    pat: vg.Str;
    loc: engine.Locale;
    cflags: number;
    subject: vg.Str;
    repl: vg.Str;
    re: engine.Regexp;
}

// runPass performs iters operations of the kind the case names.
// Each kind is a plain loop, so nothing but the engine call sits inside the timed region.
function runPass(c: Case, iters: number): void {
    switch (c.kind) {
        case "compile": {
            for (let i = 0; i < iters; i++) {
                engine.Compile(c.pat, c.loc, c.cflags);
            }
            break;
        }
        case "match": {
            const nmatch = engine.NumSub(c.re) + 1;
            for (let i = 0; i < iters; i++) {
                const pmatch = vg.make(engine.Match.elem, nmatch);
                engine.Exec(c.re, c.subject, pmatch, 0);
            }
            break;
        }
        case "replace": {
            for (let i = 0; i < iters; i++) {
                engine.ReplaceAll(c.re, c.subject, c.repl, -1, 0);
            }
            break;
        }
    }
}

async function main(): Promise<void> {
    const base = baseLocale();
    let cur = engine.LocalePOSIX();
    for await (const line of lines()) {
        const f = line.split(" ");
        if (f.length === 0 || f[0] === "") {
            continue;
        }
        switch (f[0]) {
            case "P": {
                cur = engine.LocalePOSIX();
                process.stdout.write("P 1\n");
                break;
            }
            case "L": {
                const res = engine.LocaleSelect(base, decode(need(f, 1)), decode(need(f, 2)));
                if (res[1]) {
                    cur = res[0];
                }
                process.stdout.write("L " + (res[1] ? 1 : 0) + "\n");
                break;
            }
            case "B": {
                const name = need(f, 1);
                const kind = need(f, 2);
                const iters = int(need(f, 3));
                const reps = int(need(f, 4));
                if (kind !== "compile" && kind !== "match" && kind !== "replace") {
                    process.stderr.write("unknown bench kind " + kind + "\n");
                    process.exit(1);
                }
                if (iters <= 0) {
                    process.stderr.write("bench iters must be positive\n");
                    process.exit(1);
                }
                const c: Case = {
                    kind,
                    cflags: int(need(f, 5)),
                    pat: decode(need(f, 6)),
                    subject: decode(need(f, 7)),
                    repl: decode(need(f, 8)),
                    loc: cur,
                    re: new engine.Regexp(),
                };
                const res = engine.Compile(c.pat, c.loc, c.cflags);
                if (res[1].Code !== 0) {
                    process.stdout.write(`B ${name} ${res[1].Code} 0 0\n`);
                    break;
                }
                c.re = res[0];

                const bytes0 = vg.stats.bytes;
                const count0 = vg.stats.count;
                runPass(c, iters);
                const bytes = Math.floor((vg.stats.bytes - bytes0) / iters);
                const allocs = Math.floor((vg.stats.count - count0) / iters);
                let text = `B ${name} 0 ${bytes} ${allocs}`;
                for (let r = 0; r < reps; r++) {
                    const start = process.hrtime.bigint();
                    runPass(c, iters);
                    const ns = process.hrtime.bigint() - start;
                    text += ` ${ns}`;
                }
                process.stdout.write(text + "\n");
                break;
            }
            default: {
                process.stderr.write("unknown bench command\n");
                process.exit(1);
            }
        }
    }
}

await main();
