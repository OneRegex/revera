package reference

// race_test.go sets raceEnabled under the race detector.
// That instrumentation adds allocations, and the allocation test must ignore them.
var raceEnabled bool
