package sub

import "testing"

func TestOpencodeDoctorModelFreePreference(t *testing.T) {
	for _, tc := range []struct {
		name   string
		models []string
		want   string
	}{
		{"free-before-paid-flash", []string{"paid/flash", "zen/large-free"}, "zen/large-free"},
		{"compact-free-before-other-free", []string{"zen/large-free", "paid/flash", "zen/flash-free"}, "zen/flash-free"},
		{"compact-free-keeps-live-order", []string{"zen/mini-free", "zen/flash-free", "zen/lite-free", "zen/nano-free"}, "zen/mini-free"},
		{"lite-free", []string{"zen/large-free", "zen/lite-free"}, "zen/lite-free"},
		{"nano-free", []string{"zen/large-free", "zen/nano-free"}, "zen/nano-free"},
		{"other-free-keeps-live-order", []string{"paid/flash", "zen/pro-free", "zen/haiku-free"}, "zen/pro-free"},
		{"free-in-name", []string{"paid/flash", "zen/model-free-preview"}, "paid/flash"},
		{"case-insensitive", []string{"paid/flash", "zen/large-free", "ZEN/FLASH-FREE-PREVIEW"}, "zen/large-free"},
		// IDs supplied by the Owner's host review; no live listing or API call.
		{"host-list-fragment", []string{"opencode/deepseek-v4-flash", "minimax-coding-plan/MiniMax-M3", "opencode/deepseek-v4.1-flash-free"}, "opencode/deepseek-v4.1-flash-free"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := opencodeDoctorModel(tc.models); got != tc.want {
				t.Fatalf("model=%q, want %q (live list=%v)", got, tc.want, tc.models)
			}
		})
	}
}

func TestOpencodeDoctorModelWithoutFreePreservesTiers(t *testing.T) {
	for _, tc := range []struct {
		name   string
		models []string
		want   string
	}{
		{"flash-before-mini", []string{"test/mini", "test/flash", "test/flash-second"}, "test/flash"},
		{"compact-keeps-live-order", []string{"test/pro", "test/haiku", "test/mini", "test/lite", "test/nano"}, "test/haiku"},
		{"non-pro", []string{"test/pro", "test/large", "test/other"}, "test/large"},
		{"only-pro", []string{"test/pro-first", "test/pro-second"}, "test/pro-first"},
		{"case-insensitive", []string{"test/mini", "test/FLASH"}, "test/FLASH"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := opencodeDoctorModel(tc.models); got != tc.want {
				t.Fatalf("model=%q, want %q (live list=%v)", got, tc.want, tc.models)
			}
		})
	}
}
