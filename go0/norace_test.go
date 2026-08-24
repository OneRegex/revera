package revera

// raceEnabled is set by race_test.go under the race detector, whose
// instrumentation adds allocations that the allocation test must ignore.
var raceEnabled bool
