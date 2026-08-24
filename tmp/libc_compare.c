#include <regex.h>
#include <stdio.h>
#include <string.h>

int main(void) {
    char line[4096];
    while (fgets(line, sizeof line, stdin)) {
        char *tab = strchr(line, '\t');
        char *end = strchr(line, '\n');
        if (!tab) continue;
        if (end) *end = 0;
        *tab = 0;
        const char *pattern = line;
        const char *subject = tab + 1;
        regex_t re;
        regmatch_t m[10];
        if (regcomp(&re, pattern, REG_EXTENDED)) { printf("CERR\n"); continue; }
        int rc = regexec(&re, subject, 10, m, 0);
        if (rc) printf("NOMATCH\n");
        else {
            size_t n = re.re_nsub + 1;
            for (size_t i = 0; i < n && i < 10; i++)
                printf("(%lld,%lld)", (long long)m[i].rm_so, (long long)m[i].rm_eo);
            printf("\n");
        }
        regfree(&re);
    }
    return 0;
}
