//go:build race

package vim

// raceSlowdown scales timing limits in tests: the race detector slows code
// down several times.
const raceSlowdown = 10
