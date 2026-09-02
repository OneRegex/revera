# Security policy

## Supported versions

Only the latest release of Revera receives fixes.
One release number covers the Go, Rust, Zig, C and C++ implementations, so a fix lands in all five at once.

## Reporting a vulnerability

Report a vulnerability privately through the GitHub security advisory form of the repository, at `https://github.com/oneregex/revera/security/advisories/new`.
Do not open a public issue for it.

Include the pattern, the subject, the flags and the locale that trigger the problem, and the language you observed it in.
A pattern that makes an engine read or write outside its buffers, loop without bound, or exceed the resource contract it reported is a vulnerability.
A pattern that the engine rejects with an error, or that runs slowly but within its contract, is not.

You get an acknowledgement within a week.
A fix ships as a new release of every implementation, and the advisory is published with it.
