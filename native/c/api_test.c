// Tests for the public API in revera.h.
// It builds and runs from "make test".

#include "revera.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static int failures;

// check returns what it checked.
// A caller can then stop before it reads a result that the failed check says is present.
static bool check(bool ok, const char *what) {
    if (!ok) {
        fprintf(stderr, "FAIL: %s\n", what);
        failures++;
    }
    return ok;
}

static revera_regex *compile(const char *pattern, const revera_options *options) {
    revera_error error;
    revera_regex *regex = revera_compile(pattern, strlen(pattern), options, &error);
    if (regex == NULL) {
        fprintf(stderr, "FAIL: compile %s: %s\n", pattern, error.message);
        failures++;
    }
    return regex;
}

static bool text_is(const char *subject, revera_match match, const char *want) {
    size_t n = strlen(want);
    return match.participated && match.end - match.start == n &&
           memcmp(subject + match.start, want, n) == 0;
}

static void find_and_captures(void) {
    const char *subject = "__abc12__";
    revera_regex *regex = compile("([a-z]+)([0-9]*)", NULL);
    if (regex == NULL) {
        return;
    }
    check(revera_group_count(regex) == 3, "group_count");
    revera_error error;
    check(revera_matches(regex, subject, strlen(subject), &error), "matches");
    revera_match match;
    if (check(revera_find(regex, subject, strlen(subject), &match, &error), "find")) {
        check(match.start == 2 && match.end == 7, "find offsets");
        check(text_is(subject, match, "abc12"), "find text");
    }
    revera_match groups[3];
    if (check(revera_captures(regex, subject, strlen(subject), groups, 3, &error), "captures")) {
        check(text_is(subject, groups[0], "abc12"), "group 0");
        check(text_is(subject, groups[1], "abc"), "group 1");
        check(text_is(subject, groups[2], "12"), "group 2");
    }
    revera_regex_free(regex);
}

static void absent_and_missing(void) {
    revera_error error;
    revera_regex *branches = compile("(a)|(b)", NULL);
    if (branches != NULL) {
        revera_match groups[3];
        check(revera_captures(branches, "a", 1, groups, 3, &error), "alternation captures");
        check(groups[1].participated, "taken branch");
        check(!groups[2].participated, "untaken branch");
        revera_regex_free(branches);
    }
    revera_regex *missing = compile("z+", NULL);
    if (missing != NULL) {
        revera_match match;
        check(!revera_matches(missing, "abc", 3, &error) && error.status == REVERA_OK,
              "no match");
        check(!revera_find(missing, "abc", 3, &match, &error) && error.status == REVERA_OK,
              "find without a match");
        revera_regex_free(missing);
    }
}

static void iteration(void) {
    const char *subject = "aab a aabbb";
    revera_regex *regex = compile("(a+)(b*)", NULL);
    if (regex == NULL) {
        return;
    }
    revera_error error;
    revera_iterator *iterator = revera_iterator_new(regex, subject, strlen(subject), -1, &error);
    if (!check(iterator != NULL, "iterator init")) {
        revera_regex_free(regex);
        return;
    }
    const char *want[] = {"aab", "a", "aabbb"};
    revera_match groups[3];
    size_t count = 0;
    while (revera_iterator_next(iterator, groups, 3, &error)) {
        if (count < 3) {
            check(text_is(subject, groups[0], want[count]), "iterator text");
        }
        if (count == 1) {
            check(groups[2].participated && groups[2].start == groups[2].end,
                  "iterator empty group");
        }
        count++;
    }
    check(error.status == REVERA_OK, "iterator completion");
    check(count == 3, "iterator count");
    revera_iterator_free(iterator);
    revera_regex_free(regex);
}

static void replacement(void) {
    revera_regex *regex = compile("(a+)(b*)", NULL);
    if (regex == NULL) {
        return;
    }
    revera_error error;
    size_t n;
    char *out = revera_replace_all(regex, "xaabyy", 6, "[&:\\2]", 6, &n, &error);
    check(out != NULL && n == 10 && memcmp(out, "x[aab:b]yy", 10) == 0, "replace_all");
    free(out);
    out = revera_replace_first_n(regex, "aa bb aa", 8, "X", 1, 1, &n, &error);
    check(out != NULL && n == 7 && memcmp(out, "X bb aa", 7) == 0, "replace_first_n");
    free(out);
    revera_regex_free(regex);
}

static void options(void) {
    revera_error error;
    revera_match match;
    revera_options value = {.case_insensitive = true};
    revera_regex *regex = compile("ab+", &value);
    if (regex != NULL) {
        check(revera_matches(regex, "ABBB", 4, &error), "case_insensitive");
        revera_regex_free(regex);
    }
    value = (revera_options){.newline_sensitive = true};
    regex = compile("^b", &value);
    if (regex != NULL) {
        check(revera_find(regex, "a\nbc", 4, &match, &error) && match.start == 2,
              "newline_sensitive");
        revera_regex_free(regex);
    }
    value = (revera_options){.shortest_match = true};
    regex = compile("a+", &value);
    if (regex != NULL) {
        check(revera_find(regex, "aaa", 3, &match, &error) && match.end == 1,
              "shortest_match");
        revera_regex_free(regex);
    }
    value = (revera_options){.no_captures = true};
    regex = compile("a+", &value);
    if (regex != NULL) {
        check(revera_matches(regex, "baa", 3, &error), "no_captures matches");
        check(!revera_find(regex, "baa", 3, &match, &error) &&
                  error.status == REVERA_NO_CAPTURES,
              "no_captures refuses offsets");
        revera_regex_free(regex);
    }
}

static void locales(void) {
    revera_locale *locale = revera_locale_open("cs", 2, "", 0);
    if (!check(locale != NULL, "cs locale")) {
        return;
    }
    revera_options options = {.locale = locale};
    revera_regex *regex = compile("[[.ch.]]", &options);
    if (regex != NULL) {
        revera_error error;
        check(revera_matches(regex, "ch", 2, &error), "collating element");
        revera_regex_free(regex);
    }
    revera_locale_free(locale);
    check(revera_locale_open("xx-not-there", 12, "", 0) == NULL, "unknown locale");
    check(revera_locale_count() > 1000, "locale names");
    const char *name;
    size_t name_len;
    check(revera_locale_name(0, &name, &name_len) && name != NULL && name_len > 0,
          "first locale name");
    size_t blob_size;
    check(revera_embedded_locale_data(&blob_size) != NULL && blob_size > 1000,
          "embedded locale data");
}

static void errors_and_contract(void) {
    revera_error error;
    revera_regex *bad = revera_compile("a(", 2, NULL, &error);
    check(bad == NULL, "expected compile failure");
    check(error.status == REVERA_PATTERN, "failure kind");
    check(error.offset == 2, "failure offset");
    bad = revera_compile("[[:bogus:]]", 11, NULL, &error);
    check(bad == NULL && error.status == REVERA_CHARACTER_CLASS, "class failure kind");

    revera_regex *regex = compile("(a|ab)(c|bcd)(d*)", NULL);
    if (regex == NULL) {
        return;
    }
    revera_contract big = revera_contract_for(regex, 1 << 12);
    check(big.max_input == 1 << 12, "contract max_input");
    check(big.heap_bytes > 0 && big.stack_bytes > 0 && big.steps > 0, "contract figures");
    check(big.has_solver, "contract solver");
    check(big.matcher.steps > 0, "contract matcher");
    check(revera_contract_for(regex, 16).steps < big.steps, "contract grows");
    check(revera_contract_for(regex, SIZE_MAX).max_input == (1u << 31) - 1,
          "contract clamps");
    revera_regex_free(regex);
}

int main(void) {
    find_and_captures();
    absent_and_missing();
    iteration();
    replacement();
    options();
    locales();
    errors_and_contract();
    if (failures != 0) {
        fprintf(stderr, "%d checks failed\n", failures);
        return 1;
    }
    puts("all checks passed");
    return 0;
}
