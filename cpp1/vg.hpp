// Minimal runtime for the generated Vego engine. It supplies what
// the Go runtime supplied implicitly: growable buffers, immutable
// string views, comparison helpers, and memory.
//
// Memory is explicit. Every generated function that allocates
// takes an Arena reference as its first parameter, so the runtime
// holds no state at all. The host owns the arenas and decides
// which one backs each engine call; two threads with separate
// arenas never share anything mutable.

#pragma once

#include <algorithm>
#include <array>
#include <cassert>
#include <cstdint>
#include <cstdlib>
#include <cstring>
#include <initializer_list>
#include <new>
#include <string_view>
#include <type_traits>
#include <vector>

namespace vg {

class Arena {
  public:
    Arena() = default;
    Arena(const Arena&) = delete;
    Arena& operator=(const Arena&) = delete;
    ~Arena() { reset(); }

    void* alloc(size_t n) {
        void* p = std::malloc(n ? n : 1);
        if (p == nullptr) {
            std::abort();
        }
        blocks_.push_back(p);
        return p;
    }

    void reset() {
        for (void* p : blocks_) {
            std::free(p);
        }
        blocks_.clear();
    }

  private:
    std::vector<void*> blocks_;
};

template <typename T>
T* alloc_elems(Arena& mem, int64_t n) {
    if (n < 1) {
        n = 1;
    }
    return static_cast<T*>(mem.alloc(size_t(n) * sizeof(T)));
}

// Str is an immutable byte view, the translation of a Go string.
// The zero value is the empty string. The pointer is const char* so
// string literals construct it at compile time.
struct Str {
    const char* p = nullptr;
    int64_t len = 0;

    uint8_t operator[](int64_t i) const {
        assert(i >= 0 && i < len);
        return uint8_t(p[i]);
    }

    Str sub(int64_t lo, int64_t hi) const {
        assert(0 <= lo && lo <= hi && hi <= len);
        if (p == nullptr) {
            return Str{};
        }
        return Str{p + lo, hi - lo};
    }

    Str tail(int64_t lo) const { return sub(lo, len); }
    Str head(int64_t hi) const { return sub(0, hi); }

    // Strings compare by content, like Go, so defaulted equality
    // on structs holding Str fields stays correct.
    bool operator==(const Str& o) const {
        if (len != o.len) {
            return false;
        }
        return len == 0 || std::memcmp(p, o.p, size_t(len)) == 0;
    }
};

template <size_t N>
constexpr Str lit(const char (&s)[N]) {
    return Str{s, int64_t(N - 1)};
}

// str wraps runtime bytes as a Str without copying. The caller
// keeps the bytes alive.
inline Str str(std::string_view s) {
    return Str{s.data(), int64_t(s.size())};
}

inline bool streq(Str a, Str b) {
    return a == b;
}

inline int32_t strcmp3(Str a, Str b) {
    size_t n = size_t(std::min(a.len, b.len));
    int c = n ? std::memcmp(a.p, b.p, n) : 0;
    if (c != 0) {
        return c < 0 ? -1 : 1;
    }
    if (a.len != b.len) {
        return a.len < b.len ? -1 : 1;
    }
    return 0;
}

// Slice is a Go slice header: pointer, length, capacity.
// Assignment copies the header and shares the buffer, exactly like
// Go. A null pointer is the nil slice; every allocation, even a
// zero-length one, produces a non-null pointer.
template <typename T>
struct Slice {
    T* p = nullptr;
    int64_t len = 0;
    int64_t cap = 0;

    T& operator[](int64_t i) const {
        assert(i >= 0 && i < len);
        return p[i];
    }

    Slice sub(int64_t lo, int64_t hi) const {
        assert(0 <= lo && lo <= hi && hi <= cap);
        if (p == nullptr) {
            return Slice{};
        }
        return Slice{p + lo, hi - lo, cap - lo};
    }

    Slice tail(int64_t lo) const { return sub(lo, len); }
    Slice head(int64_t hi) const { return sub(0, hi); }
};

inline int64_t len(Str s) { return s.len; }

template <typename T>
int64_t len(Slice<T> s) {
    return s.len;
}

template <typename T, size_t N>
int64_t len(const std::array<T, N>&) {
    return int64_t(N);
}

template <typename T>
Slice<T> make_cap(Arena& mem, int64_t n, int64_t c) {
    // Value initialization of every generated type is all zero
    // bytes, so one memset replaces per-element construction.
    static_assert(std::is_trivially_copyable_v<T>);
    assert(0 <= n && n <= c);
    T* p = alloc_elems<T>(mem, c);
    std::memset(p, 0, size_t(c) * sizeof(T));
    return Slice<T>{p, n, c};
}

template <typename T>
Slice<T> make(Arena& mem, int64_t n) {
    return make_cap<T>(mem, n, n);
}

template <typename T>
Slice<T> grow(Arena& mem, Slice<T> s, int64_t need) {
    int64_t newcap = std::max<int64_t>(s.cap * 2, 8);
    if (newcap < need) {
        newcap = need;
    }
    // The spare region must read as zero: Go allocates zeroed
    // memory, and extending a slice inside its capacity exposes
    // that memory. The prefix gets the live elements instead.
    T* p = alloc_elems<T>(mem, newcap);
    std::memset(p + s.len, 0, size_t(newcap - s.len) * sizeof(T));
    if (s.p != nullptr && s.len > 0) {
        std::memcpy(p, s.p, size_t(s.len) * sizeof(T));
    }
    return Slice<T>{p, s.len, newcap};
}

template <typename T>
Slice<T> append(Arena& mem, Slice<T> s, T v) {
    if (s.len == s.cap) {
        s = grow(mem, s, s.len + 1);
    }
    s.p[s.len] = v;
    s.len++;
    return s;
}

// The source of a spread append may alias the old buffer; the old
// buffer stays intact after a grow, so a plain copy is right.
template <typename T>
Slice<T> append_slice(Arena& mem, Slice<T> s, Slice<T> more) {
    if (s.len + more.len > s.cap) {
        s = grow(mem, s, s.len + more.len);
    }
    if (more.len > 0) {
        std::memmove(s.p + s.len, more.p, size_t(more.len) * sizeof(T));
    }
    s.len += more.len;
    return s;
}

inline Slice<uint8_t> append_str(Arena& mem, Slice<uint8_t> s, Str more) {
    if (s.len + more.len > s.cap) {
        s = grow(mem, s, s.len + more.len);
    }
    if (more.len > 0) {
        std::memmove(s.p + s.len, more.p, size_t(more.len));
    }
    s.len += more.len;
    return s;
}

template <typename T>
int64_t vcopy(Slice<T> dst, Slice<T> src) {
    int64_t n = std::min(dst.len, src.len);
    if (n > 0) {
        std::memmove(dst.p, src.p, size_t(n) * sizeof(T));
    }
    return n;
}

inline int64_t vcopy_str(Slice<uint8_t> dst, Str src) {
    int64_t n = std::min(dst.len, src.len);
    if (n > 0) {
        std::memmove(dst.p, src.p, size_t(n));
    }
    return n;
}

// sdiv and srem are Go's truncating division and remainder. Go
// defines MinInt / -1 as MinInt (wrapping) and MinInt % -1 as 0;
// C++ leaves that pair undefined even with -fwrapv.
template <typename T>
T sdiv(T a, T b) {
    if (b == T(-1)) {
        return T(0U - typename std::make_unsigned<T>::type(a));
    }
    return T(a / b);
}

template <typename T>
T srem(T a, T b) {
    if (b == T(-1)) {
        return 0;
    }
    return T(a % b);
}

// bytes_from_str copies a string into a fresh mutable byte buffer,
// the []uint8(s) conversion.
inline Slice<uint8_t> bytes_from_str(Arena& mem, Str s) {
    Slice<uint8_t> out = make<uint8_t>(mem, s.len);
    if (s.len > 0) {
        std::memcpy(out.p, s.p, size_t(s.len));
    }
    return out;
}

// str_dup copies a string into the arena, so the result outlives
// the buffer it came from.
inline Str str_dup(Arena& mem, Str s) {
    char* p = alloc_elems<char>(mem, s.len);
    if (s.len > 0) {
        std::memcpy(p, s.p, size_t(s.len));
    }
    return Str{p, s.len};
}

inline Str str_from_bytes(Arena& mem, Slice<uint8_t> b) {
    char* p = alloc_elems<char>(mem, b.len);
    if (b.len > 0) {
        std::memcpy(p, b.p, size_t(b.len));
    }
    return Str{p, b.len};
}

template <typename T, size_t N>
Slice<T> arr_slice(std::array<T, N>& a, int64_t lo, int64_t hi) {
    assert(0 <= lo && lo <= hi && hi <= int64_t(N));
    if (N == 0) {
        // std::array<T, 0>::data() may be null, but a Go slice of
        // a zero-length array is non-nil. Point at the array object
        // itself: it has real storage, and with cap 0 the pointer
        // is never dereferenced.
        return Slice<T>{reinterpret_cast<T*>(&a), 0, 0};
    }
    return Slice<T>{a.data() + lo, hi - lo, int64_t(N) - lo};
}

template <typename T>
Slice<T> slice_of(Arena& mem, std::initializer_list<T> elems) {
    Slice<T> out = make<T>(mem, int64_t(elems.size()));
    if (elems.size() > 0) {
        std::memcpy(out.p, elems.begin(), elems.size() * sizeof(T));
    }
    return out;
}

} // namespace vg
