//go:build !windows

package orchestrator

func isPlatformConnectionResetError(error) bool {
	return false
}
