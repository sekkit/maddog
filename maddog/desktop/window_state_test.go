package main

import "testing"

func TestIsRestorableWindowStateRejectsMinimizedSnapshots(t *testing.T) {
	tests := []struct {
		name  string
		state DesktopWindowState
		want  bool
	}{
		{
			name:  "normal",
			state: DesktopWindowState{Width: 1240, Height: 720, X: 120, Y: 80},
			want:  true,
		},
		{
			name:  "windows minimized sentinel",
			state: DesktopWindowState{Width: 157, Height: 24, X: -32000, Y: -32000},
			want:  false,
		},
		{
			name:  "too small",
			state: DesktopWindowState{Width: 399, Height: 720, X: 120, Y: 80},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRestorableWindowState(tt.state); got != tt.want {
				t.Fatalf("isRestorableWindowState(%+v) = %v, want %v", tt.state, got, tt.want)
			}
		})
	}
}
