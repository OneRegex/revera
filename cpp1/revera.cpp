#include "revera.hpp"

#include "engine.hpp"

#include <algorithm>
#include <cstdint>

namespace revera {

// revera.hpp cannot see the engine, so it states dup_max itself.
static_assert(dup_max == engine::dupMax);

namespace {

const char locale_blob[] = {
#embed "data.bin"
};

// The engine counts in int64_t.
// A size_t past that range cannot be a real bound, so clamp saturates it instead of wrapping.
int64_t clamp(size_t n) {
    return int64_t(std::min<size_t>(n, size_t(INT64_MAX)));
}

std::string_view text_of(vg::Str s) {
    if (s.p == nullptr) {
        return {};
    }
    return std::string_view(s.p, size_t(s.len));
}

Failure failure_of(int32_t code) {
    switch (code) {
    case engine::ErrBadPat:
        return Failure::Pattern;
    case engine::ErrECollate:
        return Failure::CollatingElement;
    case engine::ErrECType:
        return Failure::CharacterClass;
    case engine::ErrEEscape:
        return Failure::Escape;
    case engine::ErrESubReg:
        return Failure::BackReference;
    case engine::ErrEBrack:
        return Failure::Bracket;
    case engine::ErrEParen:
        return Failure::Paren;
    case engine::ErrEBrace:
        return Failure::Brace;
    case engine::ErrBadBR:
        return Failure::Interval;
    case engine::ErrERange:
        return Failure::Range;
    case engine::ErrESpace:
        return Failure::Capacity;
    case engine::ErrBadRpt:
        return Failure::Repeat;
    case engine::ErrENoSub:
        return Failure::NoCaptures;
    default:
        return Failure::Unknown;
    }
}

[[noreturn]] void raise(engine::Error err) {
    std::string what(text_of(engine::ErrorText(err.Code)));
    std::optional<size_t> offset;
    if (err.Pos >= 0) {
        offset = size_t(err.Pos);
        what += " at byte " + std::to_string(err.Pos);
    }
    throw Error(failure_of(err.Code), offset, what);
}

void check(engine::Error err) {
    if (err.Code != engine::ErrNone) {
        raise(err);
    }
}

Captures captures_of(std::string_view subject, vg::Slice<engine::Match> pmatch) {
    Captures out;
    out.reserve(size_t(pmatch.len));
    for (int64_t i = 0; i < pmatch.len; i++) {
        const engine::Match& m = pmatch[i];
        if (m.So < 0) {
            out.push_back(std::nullopt);
        } else {
            out.push_back(Match(subject, size_t(m.So), size_t(m.Eo)));
        }
    }
    return out;
}

BackendContract backend_of(const engine::BackendContract& b) {
    return BackendContract{uint64_t(b.HeapBytes), uint64_t(b.StackBytes), uint64_t(b.Steps)};
}

} // namespace

std::string_view embedded_locale_data() {
    return std::string_view(locale_blob, sizeof(locale_blob));
}

struct Locale::Data {
    engine::Locale loc;
};

Locale::Locale() {
    // The POSIX locale needs no data and never changes.
    // Every default-built Locale therefore shares one instance.
    static const std::shared_ptr<const Data> posix =
        std::make_shared<const Data>(Data{engine::LocalePOSIX()});
    data_ = posix;
}

std::optional<Locale> Locale::open(std::string_view name, std::string_view collation_type) {
    vg::Arena mem;
    auto res = engine::LocaleOpen(mem, vg::str(embedded_locale_data()), vg::str(name),
                                 vg::str(collation_type));
    if (!res.r1) {
        return std::nullopt;
    }
    Locale out;
    out.data_ = std::make_shared<const Data>(Data{res.r0});
    return out;
}

std::vector<std::string> Locale::names() {
    auto loaded = engine::LocaleLoad(vg::str(embedded_locale_data()));
    if (!loaded.r1) {
        return {};
    }
    engine::Locale base = loaded.r0;
    std::vector<std::string> out;
    int64_t count = engine::LocaleCount(base);
    out.reserve(size_t(count));
    for (int64_t i = 0; i < count; i++) {
        out.emplace_back(text_of(engine::LocaleName(base, i)));
    }
    return out;
}

struct Regex::Impl {
    // The arena owns the compiled program.
    // The constructor writes it once, and every search only reads it.
    vg::Arena mem;
    engine::Regexp re;
    size_t groups = 0;

    // exec runs one search in an arena of its own.
    // want is the number of offsets to fill.
    // Zero asks for existence only.
    // On a match, exec calls take before the arena goes.
    template <typename F>
    bool exec(std::string_view subject, int64_t want, F&& take) const {
        vg::Arena scratch;
        engine::Regexp copy = re;
        vg::Slice<engine::Match> pmatch;
        if (want > 0) {
            pmatch = vg::make<engine::Match>(scratch, want);
        }
        auto res = engine::Exec(scratch, copy, vg::str(subject), pmatch, 0);
        check(res.r1);
        if (!res.r0) {
            return false;
        }
        take(pmatch);
        return true;
    }

    void refuse_without_captures() const {
        if (re.flags & engine::FlagNoSub) {
            raise(engine::Error{engine::ErrENoSub, -1});
        }
    }

    // scan calls visit once per non-overlapping match, left to right.
    // The slice it passes lives in the scratch arena, and the next step rewrites it.
    template <typename F>
    void scan(std::string_view subject, F&& visit) const {
        vg::Arena hold;
        engine::Regexp copy = re;
        auto init = engine::MatchIterInit(copy, -1);
        check(init.r1);
        engine::MatchIter it = init.r0;
        vg::Slice<engine::Match> pmatch = vg::make<engine::Match>(hold, int64_t(groups));
        while (true) {
            // Each step frees the workspace of its own search.
            // One arena for the whole scan would hold every match's workspace until the scan ended.
            vg::Arena step;
            auto res = engine::MatchIterNext(step, copy, it, vg::str(subject), 0, pmatch);
            check(res.r1);
            if (!res.r0) {
                return;
            }
            visit(pmatch);
        }
    }

    std::string replace(std::string_view subject, std::string_view replacement,
                        int64_t limit) const {
        vg::Arena scratch;
        engine::Regexp copy = re;
        auto res = engine::ReplaceAll(scratch, copy, vg::str(subject), vg::str(replacement), limit, 0);
        check(res.r1);
        return std::string(text_of(res.r0));
    }
};

Regex::Regex(std::string_view pattern, const Options& options) : impl_(new Impl) {
    uint32_t flags = 0;
    if (options.case_insensitive) {
        flags |= engine::FlagICase;
    }
    if (options.newline_sensitive) {
        flags |= engine::FlagNewline;
    }
    if (options.no_captures) {
        flags |= engine::FlagNoSub;
    }
    if (options.shortest_match) {
        flags |= engine::FlagMinimal;
    }
    // The pattern goes into the arena first, so the caller may compile from a temporary and drop it.
    vg::Str source = vg::str_dup(impl_->mem, vg::str(pattern));
    auto res = engine::Compile(impl_->mem, source, options.locale.data_->loc, flags);
    check(res.r1);
    impl_->re = res.r0;
    impl_->groups = size_t(engine::NumSub(impl_->re)) + 1;
}

Regex::Regex(Regex&&) noexcept = default;
Regex& Regex::operator=(Regex&&) noexcept = default;
Regex::~Regex() = default;

size_t Regex::group_count() const noexcept {
    return impl_->groups;
}

bool Regex::matches(std::string_view subject) const {
    return impl_->exec(subject, 0, [](vg::Slice<engine::Match>) {});
}

std::optional<Match> Regex::find(std::string_view subject) const {
    impl_->refuse_without_captures();
    std::optional<Match> found;
    impl_->exec(subject, 1, [&](vg::Slice<engine::Match> pmatch) {
        found.emplace(subject, size_t(pmatch[0].So), size_t(pmatch[0].Eo));
    });
    return found;
}

std::optional<Captures> Regex::captures(std::string_view subject) const {
    impl_->refuse_without_captures();
    std::optional<Captures> found;
    impl_->exec(subject, int64_t(impl_->groups), [&](vg::Slice<engine::Match> pmatch) {
        found = captures_of(subject, pmatch);
    });
    return found;
}

std::vector<Match> Regex::find_all(std::string_view subject) const {
    std::vector<Match> out;
    impl_->scan(subject, [&](vg::Slice<engine::Match> pmatch) {
        out.emplace_back(subject, size_t(pmatch[0].So), size_t(pmatch[0].Eo));
    });
    return out;
}

std::vector<Captures> Regex::capture_all(std::string_view subject) const {
    std::vector<Captures> out;
    impl_->scan(subject, [&](vg::Slice<engine::Match> pmatch) {
        out.push_back(captures_of(subject, pmatch));
    });
    return out;
}

std::string Regex::replace_all(std::string_view subject, std::string_view replacement) const {
    return impl_->replace(subject, replacement, -1);
}

std::string Regex::replace_first_n(std::string_view subject, std::string_view replacement,
                                 size_t limit) const {
    return impl_->replace(subject, replacement, clamp(limit));
}

Contract Regex::contract(size_t max_input) const {
    engine::Regexp copy = impl_->re;
    engine::Contract c = engine::ContractFor(copy, clamp(max_input));
    Contract out{};
    out.max_input = size_t(c.MaxInput);
    out.heap_bytes = uint64_t(engine::ContractHeapBytes(c));
    out.stack_bytes = uint64_t(engine::ContractStackBytes(c));
    out.steps = uint64_t(engine::ContractSteps(c));
    out.matcher = backend_of(c.Matcher);
    if (c.HasOnePass) {
        out.one_pass = backend_of(c.OnePass);
    }
    if (c.HasSolver) {
        out.solver = backend_of(c.Solver);
    }
    return out;
}

} // namespace revera
