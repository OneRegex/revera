// A minimal user of the installed C++ package.
// It exits with 0 when the pattern compiles and the capture holds the expected text.

#include <revera/revera.hpp>

#include <iostream>

int main() {
    revera::Regex re("([a-z]+)([0-9]*)");
    auto caps = re.captures("__abc12__");
    if (!caps) {
        std::cerr << "no match\n";
        return 1;
    }
    if (!(*caps)[1] || (*caps)[1]->str() != "abc") {
        std::cerr << "group 1 is not abc\n";
        return 1;
    }
    return 0;
}
