// Differential driver for the TypeScript instantiation of the revera engine.
// It reads protocol commands on stdin, one per line, and prints one output line per command.
// dev/internal/protocol/driver.go, the Go reference implementation, defines the protocol.

import * as vg from "./vg.ts";
import * as engine from "./engine.ts";
import { baseLocale, decode, encode, int, lines, need } from "./host.ts";

function rows(pmatch: vg.Slice<engine.Match>): string {
    const parts: string[] = [];
    for (let i = 0; i < pmatch.len; i++) {
        const m = pmatch.buf[pmatch.off + i];
        parts.push(m.So + "," + m.Eo);
    }
    return parts.join(",");
}

async function main(): Promise<void> {
    const base = baseLocale();
    let cur = engine.LocalePOSIX();
    let re = new engine.Regexp();
    let valid = false;
    const out: string[] = [];
    const flush = () => {
        if (out.length > 0) {
            process.stdout.write(out.join("\n") + "\n");
            out.length = 0;
        }
    };

    for await (const line of lines()) {
        const f = line.split(" ");
        if (f.length === 0 || f[0] === "") {
            continue;
        }
        switch (f[0]) {
            case "P": {
                cur = engine.LocalePOSIX();
                out.push("P 1");
                break;
            }
            case "L": {
                const res = engine.LocaleSelect(base, decode(need(f, 1)), decode(need(f, 2)));
                if (res[1]) {
                    cur = res[0];
                }
                out.push("L " + (res[1] ? 1 : 0));
                break;
            }
            case "C": {
                const flags = int(need(f, 1));
                const res = engine.Compile(decode(need(f, 2)), cur, flags);
                if (res[1].Code !== 0) {
                    valid = false;
                    out.push(`C ${res[1].Code} ${res[1].Pos} 0`);
                    break;
                }
                re = res[0];
                valid = true;
                out.push(`C 0 0 ${engine.NumSub(re)}`);
                break;
            }
            case "X": {
                if (!valid) {
                    out.push("X ERR");
                    break;
                }
                const eflags = int(need(f, 1));
                const subject = decode(need(f, 2));
                const pmatch = vg.make(engine.Match.elem, engine.NumSub(re) + 1);
                const res = engine.Exec(re, subject, pmatch, eflags);
                if (res[1].Code !== 0) {
                    out.push(`X ${res[1].Code} 0`);
                    break;
                }
                if (!res[0]) {
                    out.push("X 0 0");
                    break;
                }
                let text = "X 0 1";
                for (let i = 0; i < pmatch.len; i++) {
                    const m = pmatch.buf[pmatch.off + i];
                    text += ` ${m.So},${m.Eo}`;
                }
                out.push(text);
                break;
            }
            case "R": {
                if (!valid) {
                    out.push("R ERR");
                    break;
                }
                const limit = int(need(f, 1));
                const eflags = int(need(f, 2));
                const repl = decode(need(f, 3));
                const subject = decode(need(f, 4));
                const res = engine.ReplaceAll(re, subject, repl, limit, eflags);
                if (res[1].Code !== 0) {
                    out.push(`R ${res[1].Code} ${res[1].Pos} -`);
                    break;
                }
                out.push("R 0 0 " + encode(res[0]));
                break;
            }
            case "I": {
                if (!valid) {
                    out.push("I ERR");
                    break;
                }
                const limit = int(need(f, 1));
                const eflags = int(need(f, 2));
                const subject = decode(need(f, 3));
                const init = engine.MatchIterInit(re, limit);
                if (init[1].Code !== 0) {
                    out.push(`I ${init[1].Code} 0`);
                    break;
                }
                const iter = init[0];
                const pmatch = vg.make(engine.Match.elem, engine.NumSub(re) + 1);
                const found: string[] = [];
                let failed = false;
                for (;;) {
                    const res = engine.MatchIterNext(re, iter, subject, eflags, pmatch);
                    if (res[1].Code !== 0) {
                        out.push(`I ${res[1].Code} 0`);
                        failed = true;
                        break;
                    }
                    if (!res[0]) {
                        break;
                    }
                    found.push(rows(pmatch));
                }
                if (failed) {
                    break;
                }
                let text = `I 0 ${found.length}`;
                if (found.length > 0) {
                    text += " " + found.join("|");
                }
                out.push(text);
                break;
            }
            case "T": {
                if (!valid) {
                    out.push("T ERR");
                    break;
                }
                const c = engine.ContractFor(re, int(need(f, 1)));
                out.push(`T ${c.HasSolver ? 1 : 0} ${engine.ContractHeapBytes(c)} ${engine.ContractStackBytes(c)} ${engine.ContractSteps(c)}`);
                break;
            }
            case "O": {
                const lo = int(need(f, 1)) | 0;
                const hi = int(need(f, 2)) | 0;
                let h = 0xcbf29ce484222325n;
                for (let r = lo; r < hi; r++) {
                    h = BigInt.asUintN(64, (h ^ BigInt(engine.localeToUpper(cur, r) >>> 0)) * 0x100000001b3n);
                    h = BigInt.asUintN(64, (h ^ BigInt(engine.localeToLower(cur, r) >>> 0)) * 0x100000001b3n);
                }
                out.push(`O ${h}`);
                break;
            }
            default: {
                process.stderr.write("unknown driver command\n");
                process.exit(1);
            }
        }
        if (out.length >= 256) {
            flush();
        }
    }
    flush();
}

await main();
