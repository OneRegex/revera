// Seed pack runner for the TypeScript instantiation.
// dev/internal/conformance/fuzzcase, the Go reference, defines the pack format and the procedure.
//
// The pack is a sequence of records, each a 4-byte little-endian length followed by that many bytes.
// Every record goes through compile, exec, replace, iteration and the contract.
// Every result is ignored; an exception is the only signal.

import { readFileSync } from "node:fs";

import { baseLocale } from "./host.ts";
import { fuzzOne } from "./fuzz.ts";

function main(): number {
    if (process.argv.length !== 3) {
        process.stderr.write("usage: fuzzcase pack-file\n");
        return 2;
    }
    const pack = new Uint8Array(readFileSync(process.argv[2]));
    const base = baseLocale();
    let pos = 0;
    let count = 0;
    while (pos < pack.length) {
        if (pack.length - pos < 4) {
            process.stderr.write(`${process.argv[2]}: truncated record header after ${count} inputs\n`);
            return 1;
        }
        const n = (pack[pos] | (pack[pos + 1] << 8) | (pack[pos + 2] << 16) | (pack[pos + 3] << 24)) >>> 0;
        pos += 4;
        if (n > pack.length - pos) {
            process.stderr.write(`${process.argv[2]}: truncated record after ${count} inputs\n`);
            return 1;
        }
        fuzzOne(base, pack.subarray(pos, pos + n));
        pos += n;
        count++;
    }
    process.stdout.write(`fuzzcase: ${count} inputs\n`);
    return 0;
}

process.exit(main());
