// Pieces that the driver, bench and fuzzcase hosts share.
// The locale blob is read once here, and baseLocale decodes it once per process.
// The hex and line helpers serve the line protocols of the driver and the bench.

import { readFileSync } from "node:fs";
import { createInterface } from "node:readline";

import * as vg from "./vg.ts";
import * as engine from "./engine.ts";

export const localeData: vg.Str = new Uint8Array(readFileSync(new URL("./data.bin", import.meta.url)));

let base: engine.Locale | null = null;

// LocaleLoad validates the whole blob, so the result is worth keeping for the life of the process.
export function baseLocale(): engine.Locale {
    if (base === null) {
        const res = engine.LocaleLoad(localeData);
        if (!res[1]) {
            process.stderr.write("embedded locale data failed to load\n");
            process.exit(1);
        }
        base = res[0];
    }
    return base;
}

// decode reverses the hex encoding of the protocols, where "-" stands for the empty string.
export function decode(tok: string): vg.Str {
    if (tok === "-") {
        return vg.EMPTY;
    }
    const n = tok.length >> 1;
    const out = new Uint8Array(n);
    for (let i = 0; i < n; i++) {
        out[i] = parseInt(tok.substring(2 * i, 2 * i + 2), 16);
    }
    return out;
}

export function encode(s: vg.Str): string {
    if (s.length === 0) {
        return "-";
    }
    let out = "";
    for (let i = 0; i < s.length; i++) {
        out += s[i].toString(16).padStart(2, "0");
    }
    return out;
}

// lines yields the lines of standard input, one at a time.
export function lines(): AsyncIterable<string> {
    return createInterface({ input: process.stdin, crlfDelay: Infinity });
}

// need returns the next token of a command, or stops the process on a malformed line.
export function need(tokens: string[], at: number): string {
    if (at >= tokens.length) {
        process.stderr.write("malformed command\n");
        process.exit(1);
    }
    return tokens[at];
}

// int parses a protocol integer; the values fit a number, since they are flags, limits and offsets.
export function int(tok: string): number {
    const v = Number(tok);
    if (!Number.isSafeInteger(v)) {
        process.stderr.write("bad integer token " + tok + "\n");
        process.exit(1);
    }
    return v;
}
