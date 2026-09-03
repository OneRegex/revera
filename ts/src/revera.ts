// POSIX.1-2024 extended regular expressions.
//
// This module is the public surface of the TypeScript instantiation of the revera engine.
// The engine itself, engine.ts, is generated from a Vego program; this file wraps it in the shape a TypeScript caller expects.
//
//     import { Regex } from "@oneregex/revera";
//     const re = new Regex("([a-z]+)([0-9]*)");
//     const caps = re.captures("__abc12__");
//     caps?.get(1)?.text  // "abc"
//
// Patterns and subjects are UTF-8.
// A subject given as a string is encoded first, and every offset the module reports is a byte offset into that encoding.
// A subject given as a Uint8Array is used as is.
// The language is the POSIX ERE language: leftmost-longest matching, no backreferences, and no Perl escapes.
// Bracket expressions read their character classes, collating elements, and equivalence classes from a Locale.
// The default locale is POSIX.
//
// Every search can throw, because a subject can exceed what the engine has capacity for.
// Regex.contract reports that capacity ahead of time.

import { readFileSync } from "node:fs";

import * as vg from "./vg.ts";
import * as engine from "./engine.ts";

const encoder = new TextEncoder();
const decoder = new TextDecoder();

const localeData: Uint8Array = new Uint8Array(readFileSync(new URL("./data.bin", import.meta.url)));

/** Returns a copy of the CLDR locale blob compiled into this package. */
export function embeddedLocaleData(): Uint8Array {
    return localeData.slice();
}

function integer(value: number, what: string): number {
    if (!Number.isInteger(value)) {
        throw new RangeError(`${what} must be an integer, not ${value}`);
    }
    return value;
}

/** The largest interval count a pattern may ask for, as in `a{0,255}`. */
export const DUP_MAX: number = engine.dupMax;

/** What went wrong; the names follow the `<regex.h>` error constants. */
export type ErrorKind =
    | "pattern"
    | "collating-element"
    | "character-class"
    | "escape"
    | "back-reference"
    | "bracket"
    | "paren"
    | "brace"
    | "interval"
    | "range"
    | "capacity"
    | "repeat"
    | "no-captures"
    | "unknown";

function kindOf(code: number): ErrorKind {
    switch (code) {
        case engine.ErrBadPat:
            return "pattern";
        case engine.ErrECollate:
            return "collating-element";
        case engine.ErrECType:
            return "character-class";
        case engine.ErrEEscape:
            return "escape";
        case engine.ErrESubReg:
            return "back-reference";
        case engine.ErrEBrack:
            return "bracket";
        case engine.ErrEParen:
            return "paren";
        case engine.ErrEBrace:
            return "brace";
        case engine.ErrBadBR:
            return "interval";
        case engine.ErrERange:
            return "range";
        case engine.ErrESpace:
            return "capacity";
        case engine.ErrBadRpt:
            return "repeat";
        case engine.ErrENoSub:
            return "no-captures";
        default:
            return "unknown";
    }
}

/**
 * A compilation or search failure.
 * `offset` is the byte offset where the failure was found, when it has one.
 * Compilation offsets point into the pattern; the escape and back-reference errors of replacements point into the replacement text.
 */
export class RegexError extends Error {
    readonly kind: ErrorKind;
    readonly code: number;
    readonly offset: number | null;

    constructor(e: engine.Error) {
        const text = decoder.decode(engine.ErrorText(e.Code));
        super(e.Pos >= 0 ? `${text} at byte ${e.Pos}` : text);
        this.name = "RegexError";
        this.kind = kindOf(e.Code);
        this.code = e.Code;
        this.offset = e.Pos >= 0 ? e.Pos : null;
    }
}

function raise(e: engine.Error): never {
    throw new RegexError(e);
}

/** A subject or pattern: text, encoded as UTF-8 on the way in, or bytes used as they are. */
export type Text = string | Uint8Array;

function bytes(text: Text): Uint8Array {
    return typeof text === "string" ? encoder.encode(text) : text;
}

/** A locale: the source of character classes, case folding, collating elements, and equivalence classes. */
export class Locale {
    readonly inner: engine.Locale;

    private constructor(inner: engine.Locale) {
        this.inner = inner;
    }

    /** Returns the POSIX locale, also called the C locale. */
    static posix(): Locale {
        return new Locale(engine.LocalePOSIX());
    }

    /**
     * Resolves a CLDR locale name against the embedded data, for example `Locale.open("cs")`.
     * An empty collation type takes the standard collation of the locale.
     * The result is null when the name or the collation type is unknown.
     */
    static open(name: string, collationType: string = ""): Locale | null {
        const res = engine.LocaleOpen(localeData, encoder.encode(name), encoder.encode(collationType));
        return res[1] ? new Locale(res[0]) : null;
    }

    /** Returns every locale name the embedded data carries. */
    static names(): string[] {
        const res = engine.LocaleLoad(localeData);
        if (!res[1]) {
            return [];
        }
        const base = res[0];
        const out: string[] = [];
        const n = engine.LocaleCount(base);
        for (let i = 0; i < n; i++) {
            out.push(decoder.decode(engine.LocaleName(base, i)));
        }
        return out;
    }
}

/** One matched span of a subject, as byte offsets. */
export class Match {
    readonly subject: Uint8Array;
    /** The byte offset where the match starts. */
    readonly start: number;
    /** The byte offset one past the end of the match. */
    readonly end: number;

    constructor(subject: Uint8Array, start: number, end: number) {
        this.subject = subject;
        this.start = start;
        this.end = end;
    }

    /** The length of the match in bytes. */
    get length(): number {
        return this.end - this.start;
    }

    /** Reports whether the match is the null string. */
    get isEmpty(): boolean {
        return this.start === this.end;
    }

    /** The matched bytes, a view of the subject. */
    get bytes(): Uint8Array {
        return this.subject.subarray(this.start, this.end);
    }

    /** The matched text, decoded as UTF-8. */
    get text(): string {
        return decoder.decode(this.bytes);
    }

    toString(): string {
        return this.text;
    }
}

/**
 * One match and the spans of its capturing groups.
 * Group 0 is the whole match, and a group that took no part in the match reads as null.
 */
export class Captures implements Iterable<Match | null> {
    readonly subject: Uint8Array;
    private readonly spans: Float64Array;

    constructor(subject: Uint8Array, spans: Float64Array) {
        this.subject = subject;
        this.spans = spans;
    }

    /** Returns group i, or null when it took no part in the match or does not exist. */
    get(i: number): Match | null {
        if (!Number.isInteger(i) || i < 0 || 2 * i + 1 >= this.spans.length) {
            return null;
        }
        const so = this.spans[2 * i];
        if (so < 0) {
            return null;
        }
        return new Match(this.subject, so, this.spans[2 * i + 1]);
    }

    /** The number of groups, counting the whole match. */
    get length(): number {
        return this.spans.length / 2;
    }

    *[Symbol.iterator](): Iterator<Match | null> {
        for (let i = 0; i < this.length; i++) {
            yield this.get(i);
        }
    }
}

/** How a search may cost, for a subject of at most the bytes given to Regex.contract. */
export interface Contract {
    /** Whether captures use the compile-time selected one-pass walk. */
    readonly hasOnePass: boolean;
    /** Whether captures require the general solver instead of the compile-time selected one-pass walk. */
    readonly hasSolver: boolean;
    /** A bound on the explicit heap allocation of one match, in bytes. */
    readonly heapBytes: bigint;
    /** An estimate of the deepest call stack of one match, in bytes. */
    readonly stackBytes: bigint;
    /** A bound on the abstract operations of one match; unit-cost operations, not nanoseconds. */
    readonly steps: bigint;
}

/** The options of a Regex. */
export interface RegexOptions {
    /** Matches upper and lower case alike, like REG_ICASE. */
    caseInsensitive?: boolean;
    /** Gives ^ and $ their line meaning and stops dot and negated brackets at a newline, like REG_NEWLINE. */
    newlineSensitive?: boolean;
    /** Compiles for a yes-or-no answer only, like REG_NOSUB; the methods that report offsets then throw. */
    noCaptures?: boolean;
    /** Makes every duplication prefer the shortest repetition. */
    shortestMatch?: boolean;
    /** The locale of the bracket expressions and of case folding; the default is POSIX. */
    locale?: Locale;
}

function flagsOf(options: RegexOptions): number {
    let flags = 0;
    if (options.caseInsensitive) {
        flags |= engine.FlagICase;
    }
    if (options.newlineSensitive) {
        flags |= engine.FlagNewline;
    }
    if (options.noCaptures) {
        flags |= engine.FlagNoSub;
    }
    if (options.shortestMatch) {
        flags |= engine.FlagMinimal;
    }
    return flags;
}

/** A compiled extended regular expression. */
export class Regex {
    private readonly re: engine.Regexp;
    private readonly groups: number;
    private readonly noSub: boolean;

    /** Compiles a pattern, or throws a RegexError. */
    constructor(pattern: Text, options: RegexOptions = {}) {
        const locale = options.locale ?? Locale.posix();
        const res = engine.Compile(bytes(pattern), locale.inner, flagsOf(options));
        if (res[1].Code !== engine.ErrNone) {
            raise(res[1]);
        }
        this.re = res[0];
        this.groups = engine.NumSub(this.re) + 1;
        this.noSub = (options.noCaptures ?? false) === true;
    }

    /** The number of groups a Captures holds, counting the whole match. */
    get capturesLength(): number {
        return this.groups;
    }

    private scratch(): vg.Slice<engine.Match> {
        return vg.make(engine.Match.elem, this.groups);
    }

    private search(subject: Uint8Array, pmatch: vg.Slice<engine.Match>): boolean {
        const res = engine.Exec(this.re, subject, pmatch, 0);
        if (res[1].Code !== engine.ErrNone) {
            raise(res[1]);
        }
        return res[0];
    }

    /** Reports whether the expression matches anywhere in the subject. */
    test(subject: Text): boolean {
        return this.search(bytes(subject), vg.NIL);
    }

    /** Returns the leftmost-longest match, or null. */
    find(subject: Text): Match | null {
        if (this.noSub) {
            raise(new engine.Error(engine.ErrENoSub, -1));
        }
        const s = bytes(subject);
        const pmatch = vg.make(engine.Match.elem, 1);
        if (!this.search(s, pmatch)) {
            return null;
        }
        const m = pmatch.buf[0];
        return new Match(s, m.So, m.Eo);
    }

    /** Returns the leftmost-longest match with its groups, or null. */
    captures(subject: Text): Captures | null {
        if (this.noSub) {
            raise(new engine.Error(engine.ErrENoSub, -1));
        }
        const s = bytes(subject);
        const pmatch = this.scratch();
        if (!this.search(s, pmatch)) {
            return null;
        }
        return new Captures(s, spansOf(pmatch));
    }

    /** Walks the non-overlapping matches, left to right. */
    *matches(subject: Text): IterableIterator<Match> {
        const s = bytes(subject);
        for (const pmatch of this.iterate(s)) {
            const m = pmatch.buf[0];
            yield new Match(s, m.So, m.Eo);
        }
    }

    /** Walks the non-overlapping matches with their groups, left to right. */
    *captureMatches(subject: Text): IterableIterator<Captures> {
        const s = bytes(subject);
        for (const pmatch of this.iterate(s)) {
            yield new Captures(s, spansOf(pmatch));
        }
    }

    private *iterate(s: Uint8Array): IterableIterator<vg.Slice<engine.Match>> {
        const init = engine.MatchIterInit(this.re, -1);
        if (init[1].Code !== engine.ErrNone) {
            raise(init[1]);
        }
        const it = init[0];
        const pmatch = this.scratch();
        for (;;) {
            const res = engine.MatchIterNext(this.re, it, s, 0, pmatch);
            if (res[1].Code !== engine.ErrNone) {
                raise(res[1]);
            }
            if (!res[0]) {
                return;
            }
            yield pmatch;
        }
    }

    /**
     * Returns the subject with every non-overlapping match replaced, like the sed s///g command.
     * In the replacement, & stands for the whole match and \1 through \9 for one group, and a backslash escapes the next character.
     * A negative limit replaces every match; otherwise at most limit matches are replaced.
     */
    replaceAll(subject: Text, replacement: Text, limit: number = -1): string {
        return decoder.decode(this.replaceAllBytes(subject, replacement, limit));
    }

    /** The same as replaceAll, returning the bytes of the result. */
    replaceAllBytes(subject: Text, replacement: Text, limit: number = -1): Uint8Array {
        const res = engine.ReplaceAll(this.re, bytes(subject), bytes(replacement), integer(limit, "limit"), 0);
        if (res[1].Code !== engine.ErrNone) {
            raise(res[1]);
        }
        return res[0];
    }

    /** Returns the subject with every non-overlapping match replaced by what the function returns for it. */
    replaceAllWith(subject: Text, replacement: (m: Captures) => string): string {
        const s = bytes(subject);
        const parts: Uint8Array[] = [];
        let last = 0;
        for (const pmatch of this.iterate(s)) {
            const caps = new Captures(s, spansOf(pmatch));
            const m = pmatch.buf[0];
            parts.push(s.subarray(last, m.So));
            parts.push(encoder.encode(replacement(caps)));
            last = m.Eo;
        }
        if (parts.length === 0) {
            return typeof subject === "string" ? subject : decoder.decode(s);
        }
        parts.push(s.subarray(last));
        let total = 0;
        for (const p of parts) {
            total += p.length;
        }
        const out = new Uint8Array(total);
        let at = 0;
        for (const p of parts) {
            out.set(p, at);
            at += p.length;
        }
        return decoder.decode(out);
    }

    /** Bounds what one search can cost on a subject of at most maxInput bytes. */
    contract(maxInput: number): Contract {
        const c = engine.ContractFor(this.re, integer(maxInput, "maxInput"));
        return {
            hasOnePass: c.HasOnePass,
            hasSolver: c.HasSolver,
            heapBytes: engine.ContractHeapBytes(c),
            stackBytes: engine.ContractStackBytes(c),
            steps: engine.ContractSteps(c),
        };
    }
}

function spansOf(pmatch: vg.Slice<engine.Match>): Float64Array {
    const spans = new Float64Array(2 * pmatch.len);
    for (let i = 0; i < pmatch.len; i++) {
        const m = pmatch.buf[pmatch.off + i];
        spans[2 * i] = m.So;
        spans[2 * i + 1] = m.Eo;
    }
    return spans;
}
