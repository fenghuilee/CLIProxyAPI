package driver_test

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/driver"
)

func TestUpstreamModel(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "normal", in: "volcengine/doubao-seedance-2-5", want: "doubao-seedance-2-5"},
		{name: "mixed case", in: "  Volcengine/Doubao-Seedance-2-5  ", want: "Doubao-Seedance-2-5"},
		{name: "nested slash remainder", in: "alibaba-cn/org/wan3.0-video", want: "org/wan3.0-video"},
		{name: "bare", in: " wan3.0-video ", want: "wan3.0-video"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := driver.UpstreamModel(tc.in); got != tc.want {
				t.Fatalf("UpstreamModel(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
