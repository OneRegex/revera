# Security policy

## Supported versions

Only the latest release of Revera receives fixes.
Security fixes are applied to every affected implementation and included in the next engine release.

## Reporting a vulnerability

GitHub private vulnerability reporting is not currently enabled for this repository.
Do not put exploit details in a public issue.
Contact the maintainer through the contact information on the [maintainer's GitHub profile](https://github.com/jedisct1) to arrange a private reporting channel.

Include the pattern, the subject, the flags and the locale that trigger the problem, and the language you observed it in.
A pattern that makes an engine read or write outside its buffers, loop without bound, or exceed a reported heap or step bound is a vulnerability.
A stack figure is an estimate rather than a bound.
A pattern that the engine rejects with an error, or that runs slowly but within its contract, is not.

You get an acknowledgement within a week.
A fix is published for each affected implementation, and an advisory is published when those updates are available.
