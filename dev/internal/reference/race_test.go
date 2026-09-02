//go:build race

package reference

func init() {
	raceEnabled = true
}
