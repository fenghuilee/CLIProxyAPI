package utils

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/aigc"
	"github.com/tidwall/gjson"
)

// ResolutionPixelRange defines upper and lower pixel boundaries for a specific resolution tier record.
type ResolutionPixelRange struct {
	Resolution string
	MinPixels  int
	MaxPixels  int
}

// ResolutionPixelRanges defines resolution tiers based on total pixels (identical to BFF portal proxy pricing).
var ResolutionPixelRanges = []ResolutionPixelRange{
	// 360p
	{Resolution: "360p", MinPixels: 0, MaxPixels: 300_000},

	// 480p / SD / 0.5k
	{Resolution: "480p", MinPixels: 300_001, MaxPixels: 600_000},
	{Resolution: "sd", MinPixels: 300_001, MaxPixels: 600_000},
	{Resolution: "0.5k", MinPixels: 300_001, MaxPixels: 600_000},

	// 720p / HD / 1k
	{Resolution: "720p", MinPixels: 600_001, MaxPixels: 1_400_000},
	{Resolution: "hd", MinPixels: 600_001, MaxPixels: 1_400_000},
	{Resolution: "1k", MinPixels: 600_001, MaxPixels: 1_400_000},

	// 1080p / FHD / 1.5k
	{Resolution: "1080p", MinPixels: 1_400_001, MaxPixels: 2_610_000},
	{Resolution: "fhd", MinPixels: 1_400_001, MaxPixels: 2_610_000},
	{Resolution: "1.5k", MinPixels: 1_400_001, MaxPixels: 2_610_000},

	// 1440p / QHD / 2k / 2.5k
	{Resolution: "1440p", MinPixels: 2_610_001, MaxPixels: 5_500_000},
	{Resolution: "2k", MinPixels: 2_610_001, MaxPixels: 5_500_000},
	{Resolution: "2.5k", MinPixels: 2_610_001, MaxPixels: 5_500_000},
	{Resolution: "qhd", MinPixels: 2_610_001, MaxPixels: 5_500_000},

	// 3k
	{Resolution: "3k", MinPixels: 5_500_001, MaxPixels: 7_500_000},

	// 2160p / 4k / UHD
	{Resolution: "4k", MinPixels: 7_500_001, MaxPixels: 12_000_000},
	{Resolution: "2160p", MinPixels: 7_500_001, MaxPixels: 12_000_000},
	{Resolution: "uhd", MinPixels: 7_500_001, MaxPixels: 12_000_000},

	// 5k
	{Resolution: "5k", MinPixels: 12_000_001, MaxPixels: 18_000_000},

	// 6k
	{Resolution: "6k", MinPixels: 18_000_001, MaxPixels: 28_000_000},

	// 4320p / 8k / FUHD
	{Resolution: "8k", MinPixels: 28_000_001, MaxPixels: 200_000_000},
	{Resolution: "4320p", MinPixels: 28_000_001, MaxPixels: 200_000_000},
	{Resolution: "fuhd", MinPixels: 28_000_001, MaxPixels: 200_000_000},
}

// CanonicalContext represents the vendor-neutral normalized parameters for an AIGC task.
type CanonicalContext struct {
	Width          int    `json:"width,omitempty"`
	Height         int    `json:"height,omitempty"`
	Pixels         int    `json:"pixels,omitempty"`
	Ratio          string `json:"ratio,omitempty"`
	ResolutionTier string `json:"resolution,omitempty"`
	DurationSec    int64  `json:"duration,omitempty"`
	QualityMode    string `json:"quality_mode,omitempty"`
	CountN         int64  `json:"n,omitempty"`
	GenerateAudio  bool   `json:"generate_audio,omitempty"`
}

// MatchResolutionByPixels matches the primary resolution tier ("480p", "720p", "1080p", "4k") from total pixels.
func MatchResolutionByPixels(pixels int) string {
	for _, r := range ResolutionPixelRanges {
		if pixels >= r.MinPixels && pixels <= r.MaxPixels {
			switch r.Resolution {
			case "360p", "480p", "sd", "0.5k":
				return "480p"
			case "720p", "hd", "1k":
				return "720p"
			case "1080p", "fhd", "1.5k":
				return "1080p"
			default:
				return "4k"
			}
		}
	}
	return "720p"
}

// ResolutionFromSize extracts resolution ("480p", "720p", "1080p", "4k") from a size string formatted as WIDTHxHEIGHT or WIDTH*HEIGHT.
func ResolutionFromSize(size string) string {
	w, h, ok := ParseDimensions(size)
	if !ok || w <= 0 || h <= 0 {
		return "720p"
	}
	return MatchResolutionByPixels(w * h)
}

// BuildCanonicalFromRawInput extracts CanonicalContext from raw input bytes.
func BuildCanonicalFromRawInput(body []byte) CanonicalContext {
	ctx := CanonicalContext{
		DurationSec:    5,
		ResolutionTier: "720p",
		Ratio:          "16:9",
		QualityMode:    "std",
		CountN:         1,
	}
	if len(body) == 0 {
		return ctx
	}

	rawSize := strings.TrimSpace(gjson.GetBytes(body, "size").String())
	if rawSize == "" {
		rawSize = strings.TrimSpace(gjson.GetBytes(body, "extra_body.size").String())
	}
	if w, h, ok := ParseDimensions(rawSize); ok {
		ctx.Width = w
		ctx.Height = h
		ctx.Pixels = w * h
	} else {
		ctx.Width = int(gjson.GetBytes(body, "width").Int())
		ctx.Height = int(gjson.GetBytes(body, "height").Int())
		if ctx.Width > 0 && ctx.Height > 0 {
			ctx.Pixels = ctx.Width * ctx.Height
		}
	}

	if ctx.Pixels > 0 {
		ctx.ResolutionTier = MatchResolutionByPixels(ctx.Pixels)
	} else if res := strings.TrimSpace(gjson.GetBytes(body, "resolution").String()); res != "" {
		ctx.ResolutionTier = strings.ToLower(res)
	} else if res := strings.TrimSpace(gjson.GetBytes(body, "extra_body.resolution").String()); res != "" {
		ctx.ResolutionTier = strings.ToLower(res)
	}

	if ratio := strings.TrimSpace(gjson.GetBytes(body, "ratio").String()); ratio != "" {
		ctx.Ratio = ratio
	} else if ratio := strings.TrimSpace(gjson.GetBytes(body, "extra_body.ratio").String()); ratio != "" {
		ctx.Ratio = ratio
	} else if ctx.Width > 0 && ctx.Height > 0 {
		ctx.Ratio = RatioFromSize(fmt.Sprintf("%dx%d", ctx.Width, ctx.Height))
	}

	sec := gjson.GetBytes(body, "seconds").Int()
	if sec <= 0 {
		sec = gjson.GetBytes(body, "extra_body.seconds").Int()
	}
	if sec <= 0 {
		sec = gjson.GetBytes(body, "duration").Int()
	}
	if sec <= 0 {
		sec = gjson.GetBytes(body, "extra_body.duration").Int()
	}
	if sec > 0 || sec == -1 {
		ctx.DurationSec = sec
	}

	if ctx.ResolutionTier == "1080p" || ctx.ResolutionTier == "4k" {
		ctx.QualityMode = "pro"
	}

	if ga := gjson.GetBytes(body, "generate_audio"); ga.Exists() {
		ctx.GenerateAudio = ga.Bool()
	} else if ga := gjson.GetBytes(body, "extra_body.generate_audio"); ga.Exists() {
		ctx.GenerateAudio = ga.Bool()
	}

	if n := gjson.GetBytes(body, "n").Int(); n > 0 {
		ctx.CountN = n
	} else if n := gjson.GetBytes(body, "extra_body.n").Int(); n > 0 {
		ctx.CountN = n
	}

	return ctx
}

// ExtractCanonical retrieves canonical context:
// 1. From gen.Metadata["canonical"] (injected by BFF via X-Canonical-Body header)
// 2. Falls back to parsing gen.PreparedInput or gen.Input
func ExtractCanonical(gen aigc.ContentGeneration) CanonicalContext {
	if gen.Metadata != nil {
		if rawCanon, ok := gen.Metadata["canonical"]; ok && rawCanon != nil {
			var ctx CanonicalContext
			switch v := rawCanon.(type) {
			case map[string]any:
				canonBytes, errM := json.Marshal(v)
				if errM == nil && json.Unmarshal(canonBytes, &ctx) == nil && (ctx.ResolutionTier != "" || ctx.Ratio != "") {
					if ctx.DurationSec <= 0 {
						ctx.DurationSec = 5
					}
					return ctx
				}
			case string:
				if json.Unmarshal([]byte(v), &ctx) == nil && (ctx.ResolutionTier != "" || ctx.Ratio != "") {
					if ctx.DurationSec <= 0 {
						ctx.DurationSec = 5
					}
					return ctx
				}
			}
		}
	}

	inputBytes := gen.Input
	if len(gen.PreparedInput) > 0 {
		inputBytes = gen.PreparedInput
	}
	return BuildCanonicalFromRawInput(inputBytes)
}
