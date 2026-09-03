import { test } from "node:test";
import assert from "node:assert/strict";

import { Captures, DUP_MAX, Locale, Regex, RegexError, embeddedLocaleData } from "../src/revera.ts";

test("find and captures", () => {
    const re = new Regex("([a-z]+)([0-9]*)");
    assert.equal(re.capturesLength, 3);
    assert.equal(re.test("__abc12__"), true);

    const m = re.find("__abc12__");
    assert.ok(m);
    assert.equal(m.start, 2);
    assert.equal(m.end, 7);
    assert.equal(m.text, "abc12");
    assert.equal(m.length, 5);
    assert.equal(m.isEmpty, false);

    const caps = re.captures("__abc12__");
    assert.ok(caps);
    assert.equal(caps.length, 3);
    assert.equal(caps.get(0)?.text, "abc12");
    assert.equal(caps.get(1)?.text, "abc");
    assert.equal(caps.get(2)?.text, "12");
    assert.deepEqual([...caps].map((c) => c?.text), ["abc12", "abc", "12"]);
});

test("a group that took no part reads as null", () => {
    const re = new Regex("(a)|(b)");
    const caps = re.captures("a");
    assert.ok(caps);
    assert.ok(caps.get(1));
    assert.equal(caps.get(2), null);
    assert.equal(caps.get(9), null);
});

test("no match reads as null", () => {
    const re = new Regex("z+");
    assert.equal(re.test("abc"), false);
    assert.equal(re.find("abc"), null);
    assert.equal(re.captures("abc"), null);
    assert.deepEqual([...re.matches("abc")], []);
});

test("iterators walk every match", () => {
    const re = new Regex("(a+)(b*)");
    const found = [...re.matches("aab a aabbb")].map((m) => m.text);
    assert.deepEqual(found, ["aab", "a", "aabbb"]);
    const groups = [...re.captureMatches("aab a")];
    assert.equal(groups.length, 2);
    assert.equal(groups[0].get(1)?.text, "aa");
    assert.equal(groups[1].get(2)?.text, "");
});

test("replacement", () => {
    const re = new Regex("(a+)(b*)");
    assert.equal(re.replaceAll("aab a aabbb", "<\\2\\1>"), "<baa> <a> <bbbaa>");
    assert.equal(re.replaceAll("aab a aabbb", "&", 1), "aab a aabbb");
    assert.equal(re.replaceAll("aab a aabbb", "x", 2), "x x aabbb");
    assert.equal(re.replaceAllWith("aab a", (c: Captures) => c.get(0)!.text.toUpperCase()), "AAB A");
    assert.equal(re.replaceAllWith("zzz", () => "never"), "zzz");
});

test("offsets are byte offsets of the UTF-8 encoding", () => {
    const re = new Regex("b+");
    const m = re.find("éb");
    assert.ok(m);
    assert.equal(m.start, 2);
    assert.equal(m.end, 3);
    assert.equal(m.text, "b");
    const raw = re.find(new Uint8Array([0xff, 0x62, 0x62]));
    assert.ok(raw);
    assert.equal(raw.start, 1);
    assert.deepEqual(raw.bytes, new Uint8Array([0x62, 0x62]));
});

test("options", () => {
    assert.equal(new Regex("ab+", { caseInsensitive: true }).test("xABBx"), true);
    assert.equal(new Regex("ab+").test("xABBx"), false);
    assert.equal(new Regex("^b").test("a\nb"), false);
    assert.equal(new Regex("^b", { newlineSensitive: true }).test("a\nb"), true);
    assert.equal(new Regex("a+", { shortestMatch: true }).find("aaa")?.text, "a");

    const yesNo = new Regex("a", { noCaptures: true });
    assert.equal(yesNo.test("a"), true);
    assert.throws(() => yesNo.find("a"), (e: unknown) => e instanceof RegexError && e.kind === "no-captures");
});

test("compilation errors carry a kind and an offset", () => {
    const kind = (pattern: string, want: string) =>
        assert.throws(() => new Regex(pattern), (e: unknown) => e instanceof RegexError && e.kind === want);
    kind("(a", "paren");
    kind("[a", "bracket");
    kind("a{1", "brace");
    kind("[[:nope:]]", "character-class");
    kind("[[.xx.]]", "collating-element");
    kind("[z-a]", "range");
    kind("a{3,1}", "interval");
    kind("*a", "repeat");
    assert.throws(() => new Regex("a("), (e: unknown) => e instanceof RegexError && e.kind === "pattern" && e.offset === 2);
    try {
        new Regex("a\\");
        assert.fail("expected an error");
    } catch (e) {
        assert.ok(e instanceof RegexError);
        assert.equal(e.kind, "escape");
        assert.match(e.message, /backslash/);
    }
});

test("locales", () => {
    const cs = Locale.open("cs");
    assert.ok(cs);
    assert.equal(Locale.open("no-such-locale"), null);
    assert.ok(Locale.names().includes("cs"));
    assert.ok(embeddedLocaleData().length > 0);

    // In Czech collation, "ch" is one collating element, so [[.ch.]] matches both bytes.
    const re = new Regex("[[.ch.]]", { locale: cs });
    const m = re.find("xchx");
    assert.ok(m);
    assert.equal(m.text, "ch");

    const tr = Locale.open("tr");
    assert.ok(tr);
    assert.equal(new Regex("i", { caseInsensitive: true, locale: tr }).test("I"), false);
    assert.equal(new Regex("i", { caseInsensitive: true, locale: tr }).test("İ"), true);
});

test("contract", () => {
    const re = new Regex("(a|b)*c");
    const c = re.contract(1000);
    assert.equal(typeof c.hasOnePass, "boolean");
    assert.equal(typeof c.hasSolver, "boolean");
    assert.ok(c.heapBytes > 0n);
    assert.ok(c.stackBytes > 0n);
    assert.ok(c.steps > 0n);
    assert.equal(DUP_MAX, 255);

    const onePass = new Regex("(abc+)").contract(1000);
    assert.equal(onePass.hasOnePass, true);
    assert.equal(onePass.hasSolver, false);
    assert.equal(onePass.heapBytes, 37_757n);
    assert.equal(onePass.stackBytes, 6_144n);
    assert.equal(onePass.steps, 937_980n);

    const solver = new Regex("(a|ab)(c|bcd)(d*)").contract(1000);
    assert.equal(solver.hasOnePass, false);
    assert.equal(solver.hasSolver, true);

    const matcherOnly = new Regex("a*").contract(1000);
    assert.equal(matcherOnly.hasOnePass, false);
    assert.equal(matcherOnly.hasSolver, false);
});

test("numeric arguments must be integers", () => {
    const re = new Regex("a");
    assert.throws(() => re.replaceAll("aaa", "x", 1.5), RangeError);
    assert.throws(() => re.replaceAll("aaa", "x", NaN), RangeError);
    assert.throws(() => re.contract(1.5), RangeError);
    assert.equal(new Regex("(a)(b)").captures("ab")?.get(0.5), null);
    const blob = embeddedLocaleData();
    blob[0] ^= 0xff;
    assert.ok(Locale.open("cs"));
});

test("interval bounds", () => {
    assert.equal(new Regex("a{2,3}").find("aaaa")?.text, "aaa");
    assert.throws(() => new Regex(`a{0,${DUP_MAX + 1}}`), RegexError);
});
