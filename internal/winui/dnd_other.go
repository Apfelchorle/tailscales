//go:build !windows

package winui

import "gioui.org/io/event"

// Non-Windows builds render the same UI but have no native Explorer drop
// target. The file picker path remains available there.

func nativeWindowEvent(event.Event, chan<- []string) {}

func NativeDragOver() bool { return false }

func NativeDropMode() string { return "unavailable on this platform" }
