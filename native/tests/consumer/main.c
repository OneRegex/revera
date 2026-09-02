// A minimal user of the installed C package.
// It exits with 0 when the pattern compiles and the capture lands where it should.

#include <revera/revera.h>

#include <stdio.h>

int main(void) {
    const char pattern[] = "([a-z]+)([0-9]*)";
    revera_error error;
    revera_regex *re = revera_compile(pattern, sizeof(pattern) - 1, NULL, &error);
    if (re == NULL) {
        fprintf(stderr, "compile failed: %s\n", error.message);
        return 1;
    }
    revera_match groups[3];
    int status = 0;
    if (!revera_captures(re, "__abc12__", 9, groups, 3, &error)) {
        fprintf(stderr, "no match\n");
        status = 1;
    } else if (groups[1].start != 2 || groups[1].end != 5) {
        fprintf(stderr, "group 1 is [%zu, %zu), expected [2, 5)\n", groups[1].start, groups[1].end);
        status = 1;
    }
    revera_regex_free(re);
    return status;
}
