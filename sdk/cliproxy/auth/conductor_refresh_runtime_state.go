package auth

import "time"

func shouldPreserveModelRuntimeState(dst *ModelState, src *ModelState, now time.Time) bool {
	if !activeModelRuntimeState(src, now) {
		return false
	}
	if dst == nil {
		return true
	}
	if dst.Status == StatusDisabled {
		return false
	}
	return !activeModelRuntimeState(dst, now)
}

func shouldPreserveAuthRuntimeState(dst *Auth, src *Auth, now time.Time) bool {
	if !activeAuthRuntimeState(src, now) {
		return false
	}
	if dst.Disabled || dst.Status == StatusDisabled {
		return false
	}
	return !activeAuthRuntimeState(dst, now)
}
