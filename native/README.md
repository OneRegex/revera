# Revera for C and C++

Revera is a regex engine for POSIX extended regular expressions (ERE) with leftmost-longest matching and CLDR locales.
This directory holds the two native ports, one in C11 and one in C++20, and one CMake package that builds and installs both.

`c/` and `cpp/` each have their own README, plain Makefile, and verification tools.
The CMake package is the way to use Revera from another project.

## What the package provides

| Item              | C                   | C++                   |
| ----------------- | ------------------- | --------------------- |
| CMake target      | `Revera::C`         | `Revera::CXX`         |
| Library           | `librevera_c`       | `librevera_cxx`       |
| Header            | `<revera/revera.h>` | `<revera/revera.hpp>` |
| pkg-config module | `revera-c`          | `revera-cxx`          |
| Language level    | C11                 | C++20                 |

The two libraries are independent.
A C++ project that links `Revera::CXX` does not need `Revera::C`.

The package needs CMake 3.21 or later and either Clang or GCC.
The CMake build stops on any other compiler.
The engines rely on wrapping signed arithmetic, and the build passes `-fwrapv` to both compilers.
The generated engines and the locale data are part of the tree, so the build needs no Go toolchain.

## Build, test, and install

Run these from this directory.

```sh
cmake -S . -B build -DBUILD_TESTING=ON
cmake --build build
ctest --test-dir build --output-on-failure
cmake --install build --prefix /opt/revera
```

The build honors `BUILD_SHARED_LIBS` and `CMAKE_BUILD_TYPE` like any other CMake project.
`CMAKE_INSTALL_PREFIX` sets the default prefix, and `--prefix` on the install line overrides it.

The install puts these files under the prefix:

| Path                | Content                           |
| ------------------- | --------------------------------- |
| `include/revera/`   | `revera.h` and `revera.hpp`       |
| `lib/`              | `librevera_c` and `librevera_cxx` |
| `lib/cmake/Revera/` | `ReveraConfig.cmake` and targets  |
| `lib/pkgconfig/`    | `revera-c.pc` and `revera-cxx.pc` |
| `share/doc/revera/` | this README and the licenses      |

`lib/` stands for `CMAKE_INSTALL_LIBDIR`, so it reads `lib64/` on the distributions that use that name.

## Locale data and `#embed`

Both engines carry `data.bin`, the CLDR locale tables, inside the library.
The plain Makefiles load it with `#embed`, which needs clang 19 or gcc 15.
The CMake build does not depend on that.
At configure time it turns `data.bin` into a byte list in `build/revera_locale_data.inc` and compiles the two libraries with `REVERA_LOCALE_DATA_INC` set to that file.
Any clang or gcc release that speaks C11 and C++20 can build the package this way.

The option `REVERA_USE_EMBED` turns this off and lets `#embed` do the work:

```sh
cmake -S . -B build -DREVERA_USE_EMBED=ON
```

Without CMake, a build that has no `#embed` can define `REVERA_LOCALE_DATA_INC` itself.
The value is a quoted path to a file that lists the bytes of `data.bin` as `0x..,` items.
The initializer body in `xxd -i` output has the required form; its declaration and length lines do not belong in the file.

The port Makefiles inherit make's `CC` and `CXX` variables, which normally resolve to `cc` and `c++`.
Set them explicitly when those defaults do not support `#embed`, for example `make CC=clang-19 all` in `c/` and `make CXX=clang++-19 all` in `cpp/`.

## Using the package

With CMake:

```cmake
find_package(Revera CONFIG REQUIRED)
target_link_libraries(app PRIVATE Revera::C)
```

`find_package` also accepts `COMPONENTS C` or `COMPONENTS CXX`, but both libraries always install together.

```cmake
find_package(Revera CONFIG REQUIRED COMPONENTS CXX)
target_link_libraries(app PRIVATE Revera::CXX)
```

With pkg-config:

```sh
cc app.c $(pkg-config --cflags --libs revera-c)
c++ -std=c++20 app.cpp $(pkg-config --cflags --libs revera-cxx)
```

The `.pc` files find the prefix from their own location, so a moved prefix still works as long as `lib` stays inside it.
An absolute `CMAKE_INSTALL_LIBDIR` outside the prefix gets the configured prefix instead.

## Tests

`ctest` runs three tests:

- `revera_c_api` builds and runs `c/api_test.c` against `Revera::C`.
- `revera_cxx_api` builds and runs `cpp/api_test.cpp` against `Revera::CXX`.
- `revera_consumer` is the external-consumer test.
  It installs the package into `build/consumer-stage/prefix`, configures `tests/consumer/` against that prefix with `find_package(Revera CONFIG REQUIRED)`, builds a small C program and a small C++ program, and runs them.
  It uses the same generator and compilers as the main build.
  `cmake/ConsumerTest.cmake` drives it with `cmake -P`.

`tests/consumer/` is also a template for a project that uses an installed Revera.

The port Makefiles build the API tests and developer binaries, and provide separate sanitizer and libFuzzer targets.
The conformance kit in `dev/` runs the probe, differential corpus, stress and fuzz-seed checks against those binaries.
The port READMEs give the commands.

## Release archive

From the repository root, `make dist` creates `tmp/dist/revera-native-VERSION.tar.gz` from the recorded commit.
The archive holds this CMake project, both public APIs, the generated engines, `data.bin`, tests, and copies of the MIT and Unicode License v3 texts.
A consumer unpacks its single top-level directory and runs the CMake commands above.
The release command refuses tracked changes, an undated changelog entry, stale license copies, or a version in `CMakeLists.txt` that differs from the Zig or Cargo manifest.
`ReveraConfigVersion.cmake` reports that version to `find_package`, and CMake installs both license files under `share/doc/revera/`.
