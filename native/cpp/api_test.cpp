// Tests for the public API in revera.hpp.
// It builds and runs from "make test".

#include "revera.hpp"

#include <atomic>
#include <cstdio>
#include <string>
#include <thread>
#include <vector>

// The thread test calls check() from several threads at once.
static std::atomic<int> failures{0};

// check returns what it checked.
// A caller can then stop before it reads a result that the failed check says is not there.
static bool check(bool ok, const char* what) {
    if (!ok) {
        std::fprintf(stderr, "FAIL: %s\n", what);
        failures++;
    }
    return ok;
}

static void find_and_captures() {
    revera::Regex re("([a-z]+)([0-9]*)");
    check(re.group_count() == 3, "group_count");
    check(re.matches("__abc12__"), "matches");

    auto m = re.find("__abc12__");
    if (!check(m.has_value(), "find")) {
        return;
    }
    check(m->start() == 2 && m->end() == 7, "find offsets");
    check(m->str() == "abc12", "find text");
    check(m->size() == 5 && !m->empty(), "find size");

    auto caps = re.captures("__abc12__");
    if (!check(caps.has_value(), "captures")) {
        return;
    }
    check(caps->size() == 3, "captures size");
    check((*caps)[0]->str() == "abc12", "group 0");
    check((*caps)[1]->str() == "abc", "group 1");
    check((*caps)[2]->str() == "12", "group 2");
}

static void absent_group_is_empty() {
    revera::Regex re("(a)|(b)");
    auto caps = re.captures("a");
    if (!check(caps.has_value(), "alternation captures")) {
        return;
    }
    check((*caps)[1].has_value(), "taken branch");
    check(!(*caps)[2].has_value(), "untaken branch");
}

static void no_match_is_empty() {
    revera::Regex re("z+");
    check(!re.matches("abc"), "no match");
    check(!re.find("abc").has_value(), "find without a match");
    check(!re.captures("abc").has_value(), "captures without a match");
    check(re.find_all("abc").empty(), "find_all without a match");
}

static void find_all_walks_every_match() {
    revera::Regex re("(a+)(b*)");
    auto all = re.find_all("aab a aabbb");
    if (!check(all.size() == 3, "find_all count")) {
        return;
    }
    check(all[0].str() == "aab" && all[1].str() == "a" && all[2].str() == "aabbb",
          "find_all text");

    auto rows = re.capture_all("aab a");
    if (!check(rows.size() == 2, "capture_all count")) {
        return;
    }
    check(rows[0][1]->str() == "aa" && rows[0][2]->str() == "b", "capture_all groups");
    // (b*) takes part in the match of "a".
    // It captures the null string, which is not the same as taking no part.
    check(rows[1][2]->str().empty(), "capture_all empty group");
}

static void replacement() {
    revera::Regex re("(a+)(b*)");
    check(re.replace_all("xaabyy", "[&:\\2]") == "x[aab:b]yy", "replace_all");
    check(re.replace_first_n("aa bb aa", "X", 1) == "X bb aa", "replace_first_n");
    check(re.replace_all("xyz", "X") == "xyz", "replace_all without a match");
}

static void options() {
    revera::Regex icase("ab+", {.case_insensitive = true});
    check(icase.matches("ABBB"), "case_insensitive");

    revera::Regex lines("^b", {.newline_sensitive = true});
    auto at_line_start = lines.find("a\nbc");
    check(at_line_start && at_line_start->start() == 2, "newline_sensitive");

    revera::Regex shortest("a+", {.shortest_match = true});
    auto one = shortest.find("aaa");
    check(one && one->end() == 1, "shortest_match");

    revera::Regex plain("a+", {.no_captures = true});
    check(plain.matches("baa"), "no_captures still matches");
    try {
        plain.find("baa");
        check(false, "no_captures must refuse offsets");
    } catch (const revera::Error& e) {
        check(e.failure() == revera::Failure::NoCaptures, "no_captures failure kind");
    }
}

static void locales() {
    auto cs = revera::Locale::open("cs");
    if (!check(cs.has_value(), "cs locale")) {
        return;
    }
    revera::Regex re("[[.ch.]]", {.locale = *cs});
    check(re.matches("ch"), "collating element");
    check(!revera::Locale::open("xx-not-there").has_value(), "unknown locale");
    check(revera::Locale::names().size() > 1000, "locale names");
}

static void errors_carry_a_kind_and_a_position() {
    try {
        revera::Regex re("a(");
        check(false, "expected a compile failure");
    } catch (const revera::Error& e) {
        check(e.failure() == revera::Failure::Pattern, "failure kind");
        check(e.offset() == 2, "failure offset");
        check(std::string(e.what()) == "invalid regular expression at byte 2", "failure text");
    }
    try {
        revera::Regex re("[[:bogus:]]");
        check(false, "expected a class failure");
    } catch (const revera::Error& e) {
        check(e.failure() == revera::Failure::CharacterClass, "class failure kind");
    }
}

static void contract_grows_with_the_input_bound() {
    revera::Regex re("(a|ab)(c|bcd)(d*)");
    revera::Contract big = re.contract(1 << 12);
    check(big.max_input == 1 << 12, "contract max_input");
    check(big.heap_bytes > 0 && big.stack_bytes > 0 && big.steps > 0, "contract figures");
    check(!big.one_pass.has_value() && big.solver.has_value(), "contract solver");
    check(big.matcher.steps > 0, "contract matcher");
    check(re.contract(16).steps < big.steps, "contract grows");
    // An absurd bound clamps to the subject limit of the engine.
    check(re.contract(SIZE_MAX).max_input == (1u << 31) - 1, "contract clamps");

    revera::Contract one_pass = revera::Regex("(abc+)").contract(1000);
    check(one_pass.one_pass.has_value() && !one_pass.solver.has_value(),
          "one-pass contract backend");
    check(one_pass.heap_bytes == 37757 && one_pass.stack_bytes == 6144 &&
              one_pass.steps == 937980,
          "one-pass contract figures");
}

static void one_regex_serves_several_threads() {
    revera::Regex re("[0-9]+");
    std::vector<std::thread> threads;
    for (int i = 0; i < 4; i++) {
        threads.emplace_back([&re] {
            for (int k = 0; k < 200; k++) {
                auto found = re.find("ab 1234 cd");
                check(found && found->str() == "1234", "threaded find");
            }
        });
    }
    for (std::thread& t : threads) {
        t.join();
    }
}

int main() {
    find_and_captures();
    absent_group_is_empty();
    no_match_is_empty();
    find_all_walks_every_match();
    replacement();
    options();
    locales();
    errors_carry_a_kind_and_a_position();
    contract_grows_with_the_input_bound();
    one_regex_serves_several_threads();
    if (failures.load() != 0) {
        std::fprintf(stderr, "%d checks failed\n", failures.load());
        return 1;
    }
    std::puts("all checks passed");
    return 0;
}
