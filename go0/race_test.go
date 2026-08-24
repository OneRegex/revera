//go:build race

package revera

func init() {
	raceEnabled = true
}
