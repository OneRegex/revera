// The bytes of data.bin, for use inside an array initializer.
//
// revera.c and host.h both write
//
//     static const char blob[] = {
//     #include "locale_data.h"
//     };
//
// and this file picks the way the bytes get there.
// A build that defines REVERA_LOCALE_DATA_INC names a file that holds the bytes as a comma-separated list.
// The CMake build makes that file at configure time.
// Every other build needs #embed, which clang 19 and gcc 15 provide.
//
// This file has no include guard on purpose, and it must not be included anywhere else.

#if defined(REVERA_LOCALE_DATA_INC)
#include REVERA_LOCALE_DATA_INC
#elif defined(__has_embed)
#if __has_embed("data.bin")
#embed "data.bin"
#else
#error "data.bin is not next to locale_data.h; restore it, or define REVERA_LOCALE_DATA_INC to a file that lists its bytes"
#endif
#else
#error "this compiler has no #embed; use clang 19 or gcc 15, or define REVERA_LOCALE_DATA_INC to a file that lists the bytes of data.bin"
#endif
