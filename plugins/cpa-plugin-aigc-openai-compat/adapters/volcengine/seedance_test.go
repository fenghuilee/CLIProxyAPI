package volcengine_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/MoeForge/moego/ojson"
	_ "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/adapters/openaicompat"
	_ "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/adapters/volcengine"
	aigcdriver "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/driver"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

func TestDriver_VolcengineSeedanceFlow(t *testing.T) {
	ctx := context.Background()
	driver := aigcdriver.NewDriver()

	// 1. PrepareSubmit
	input := aigc.GenerationSubmitInput{
		Generation: aigc.ContentGeneration{
			Kind:     aigc.ContentKindVideo,
			Model:    "volcengine/doubao-seedance-2-5",
			Provider: "volcengine-seedance",
			Input:    []byte(`{"prompt":"a cinematic drone flying over mountains","seconds":5,"size":"1920x1080"}`),
		},
	}

	req, err := driver.PrepareSubmit(ctx, input)
	if err != nil {
		t.Fatalf("PrepareSubmit error: %v", err)
	}

	if ojson.GetBytes(req.Body, "model").String() != "doubao-seedance-2-5" {
		t.Fatalf("model = %q, want doubao-seedance-2-5", ojson.GetBytes(req.Body, "model").String())
	}
	if ojson.GetBytes(req.Body, "ratio").String() != "16:9" {
		t.Fatalf("ratio = %q, want 16:9", ojson.GetBytes(req.Body, "ratio").String())
	}
	if ojson.GetBytes(req.Body, "resolution").String() != "1080p" {
		t.Fatalf("resolution = %q, want 1080p", ojson.GetBytes(req.Body, "resolution").String())
	}

	// 2. ParseSubmit (Task ID returned)
	submitResp := aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Metadata:   map[string]any{"provider": "volcengine-seedance"},
		Body:       []byte(`{"id":"task_seedance_123","status":"running"}`),
	}
	submitRes, errSubmit := driver.ParseSubmit(ctx, submitResp)
	if errSubmit != nil {
		t.Fatalf("ParseSubmit error: %v", errSubmit)
	}
	if submitRes.ProviderTaskID != "task_seedance_123" {
		t.Fatalf("ProviderTaskID = %q, want task_seedance_123", submitRes.ProviderTaskID)
	}

	// 3. Poll Complete
	pollResp := aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Metadata:   map[string]any{"provider": "volcengine-seedance"},
		Body: []byte(`{
			"id": "task_seedance_123",
			"status": "succeeded",
			"content": {
				"video_url": "https://tos.volces.com/drone_1080p.mp4",
				"last_frame_url": "https://tos.volces.com/last_frame.png"
			}
		}`),
	}
	pollRes, errPoll := driver.ParsePoll(ctx, pollResp)
	if errPoll != nil {
		t.Fatalf("ParsePoll error: %v", errPoll)
	}
	if pollRes.Status != aigc.StatusSucceeded {
		t.Fatalf("Status = %v, want Succeeded", pollRes.Status)
	}
	if len(pollRes.Artifacts) != 2 {
		t.Fatalf("Artifacts count = %d, want 2", len(pollRes.Artifacts))
	}
	if pollRes.Artifacts[0].ArtifactType != "output_video" || pollRes.Artifacts[0].URI != "https://tos.volces.com/drone_1080p.mp4" {
		t.Fatalf("Artifact[0] = %+v", pollRes.Artifacts[0])
	}

	// Test Ark output[0].uri format
	arkPollResp := aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Metadata:   map[string]any{"provider": "volcengine-seedance"},
		Body: []byte(`{
			"id": "cgt-20260825102931-gd2rv",
			"status": "succeeded",
			"output": [
				{
					"uri": "https://ark-content.tos-cn-beijing.volces.com/drone_tos.mp4"
				}
			]
		}`),
	}
	arkPollRes, errArkPoll := driver.ParsePoll(ctx, arkPollResp)
	if errArkPoll != nil {
		t.Fatalf("ParsePoll error: %v", errArkPoll)
	}
	if arkPollRes.Status != aigc.StatusSucceeded {
		t.Fatalf("Status = %v, want Succeeded", arkPollRes.Status)
	}
	if len(arkPollRes.Artifacts) != 1 || arkPollRes.Artifacts[0].URI != "https://ark-content.tos-cn-beijing.volces.com/drone_tos.mp4" {
		t.Fatalf("arkPollRes.Artifacts = %+v, want drone_tos.mp4", arkPollRes.Artifacts)
	}
}

func TestDriver_VolcengineSeedanceFlow_StandardOpenAIAndMultiReference(t *testing.T) {
	ctx := context.Background()
	driver := aigcdriver.NewDriver()

	// 1. OpenAI format with multi-image input_reference and extra_body
	input := aigc.GenerationSubmitInput{
		Generation: aigc.ContentGeneration{
			Kind:     aigc.ContentKindVideo,
			Model:    "volcengine/doubao-seedance-2-0-mini",
			Provider: "volcengine-seedance",
			Input: []byte(`{
				"model": "volcengine/doubao-seedance-2-0-mini",
				"prompt": "【参考图】@图片1 和 @图片2 对话",
				"seconds": "15",
				"size": "1280x720",
				"input_reference": [
					"asset://asset-1",
					"asset://asset-2",
					"asset://asset-3"
				],
				"extra_body": {
					"generate_audio": true,
					"return_last_frame": true,
					"tools": [{"type": "web_search"}]
				}
			}`),
		},
	}

	req, err := driver.PrepareSubmit(ctx, input)
	if err != nil {
		t.Fatalf("PrepareSubmit error: %v", err)
	}

	if ojson.GetBytes(req.Body, "model").String() != "doubao-seedance-2-0-mini" {
		t.Fatalf("model = %q, want doubao-seedance-2-0-mini", ojson.GetBytes(req.Body, "model").String())
	}
	if ojson.GetBytes(req.Body, "duration").Int() != 15 {
		t.Fatalf("duration = %d, want 15", ojson.GetBytes(req.Body, "duration").Int())
	}
	if ojson.GetBytes(req.Body, "ratio").String() != "16:9" {
		t.Fatalf("ratio = %q, want 16:9", ojson.GetBytes(req.Body, "ratio").String())
	}
	if ojson.GetBytes(req.Body, "resolution").String() != "720p" {
		t.Fatalf("resolution = %q, want 720p", ojson.GetBytes(req.Body, "resolution").String())
	}
	if !ojson.GetBytes(req.Body, "generate_audio").Bool() {
		t.Fatalf("generate_audio = false, want true")
	}
	if !ojson.GetBytes(req.Body, "return_last_frame").Bool() {
		t.Fatalf("return_last_frame = false, want true")
	}

	contentItems := ojson.GetBytes(req.Body, "content").Array()
	if len(contentItems) != 4 {
		t.Fatalf("content items count = %d, want 4", len(contentItems))
	}
	if contentItems[0].Get("type").String() != "text" || contentItems[0].Get("text").String() != "【参考图】@图片1 和 @图片2 对话" {
		t.Fatalf("content[0] text = %+v", contentItems[0])
	}
	for i := 1; i <= 3; i++ {
		item := contentItems[i]
		if item.Get("type").String() != "image_url" {
			t.Fatalf("content[%d] type = %q, want image_url", i, item.Get("type").String())
		}
		if item.Get("role").String() != "reference_image" {
			t.Fatalf("content[%d] role = %q, want reference_image (MUST NOT be last_frame)", i, item.Get("role").String())
		}
	}
}

func TestDriver_VolcengineSeedanceFlow_MultimodalReference(t *testing.T) {
	ctx := context.Background()
	driver := aigcdriver.NewDriver()

	input := aigc.GenerationSubmitInput{
		Generation: aigc.ContentGeneration{
			Kind:     aigc.ContentKindVideo,
			Model:    "volcengine/doubao-seedance-2-5",
			Provider: "volcengine-seedance",
			Input: []byte(`{
				"prompt": "Multi-modal video with image, video and audio reference",
				"seconds": 10,
				"input_reference": [
					"https://example.com/character.png",
					"https://example.com/motion.mp4",
					"https://example.com/voice.mp3"
				]
			}`),
		},
	}

	req, err := driver.PrepareSubmit(ctx, input)
	if err != nil {
		t.Fatalf("PrepareSubmit error: %v", err)
	}

	contentItems := ojson.GetBytes(req.Body, "content").Array()
	if len(contentItems) != 4 {
		t.Fatalf("content items count = %d, want 4", len(contentItems))
	}

	if contentItems[1].Get("type").String() != "image_url" || contentItems[1].Get("role").String() != "reference_image" {
		t.Fatalf("content[1] = %+v, want image_url with reference_image", contentItems[1])
	}
	if contentItems[2].Get("type").String() != "video_url" || contentItems[2].Get("role").String() != "reference_video" {
		t.Fatalf("content[2] = %+v, want video_url with reference_video", contentItems[2])
	}
	if contentItems[3].Get("type").String() != "audio_url" || contentItems[3].Get("role").String() != "reference_audio" {
		t.Fatalf("content[3] = %+v, want audio_url with reference_audio", contentItems[3])
	}
}

func TestDriver_VolcengineSeedanceFlow_StandardOpenAISizeAndSecondsMapping(t *testing.T) {
	ctx := context.Background()
	driver := aigcdriver.NewDriver()

	tests := []struct {
		name           string
		inputJSON      string
		wantDuration   int64
		wantRatio      string
		wantResolution string
	}{
		{
			name:           "1080p landscape 16:9",
			inputJSON:      `{"prompt":"landscape 1080p","seconds":8,"size":"1920x1080"}`,
			wantDuration:   8,
			wantRatio:      "16:9",
			wantResolution: "1080p",
		},
		{
			name:           "720p portrait 9:16",
			inputJSON:      `{"prompt":"portrait 720p","seconds":6,"size":"720x1280"}`,
			wantDuration:   6,
			wantRatio:      "9:16",
			wantResolution: "720p",
		},
		{
			name:           "480p landscape 16:9 854x480",
			inputJSON:      `{"prompt":"landscape 480p","seconds":9,"size":"854x480"}`,
			wantDuration:   9,
			wantRatio:      "16:9",
			wantResolution: "480p",
		},
		{
			name:           "480p portrait 9:16 480x854",
			inputJSON:      `{"prompt":"portrait 480p","seconds":5,"size":"480x854"}`,
			wantDuration:   5,
			wantRatio:      "9:16",
			wantResolution: "480p",
		},
		{
			name:           "480p 4:3 640x480",
			inputJSON:      `{"prompt":"4:3 480p","seconds":5,"size":"640x480"}`,
			wantDuration:   5,
			wantRatio:      "4:3",
			wantResolution: "480p",
		},
		{
			name:           "4k landscape 16:9 3840x2160",
			inputJSON:      `{"prompt":"4k landscape","seconds":5,"size":"3840x2160"}`,
			wantDuration:   5,
			wantRatio:      "16:9",
			wantResolution: "4k",
		},
		{
			name:           "square 1:1 with extra_body ratio override",
			inputJSON:      `{"prompt":"square","seconds":4,"size":"1024x1024","extra_body":{"ratio":"adaptive"}}`,
			wantDuration:   4,
			wantRatio:      "adaptive",
			wantResolution: "720p",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := aigc.GenerationSubmitInput{
				Generation: aigc.ContentGeneration{
					Kind:     aigc.ContentKindVideo,
					Model:    "volcengine/doubao-seedance-2-5",
					Provider: "volcengine-seedance",
					Input:    []byte(tc.inputJSON),
				},
			}
			req, err := driver.PrepareSubmit(ctx, input)
			if err != nil {
				t.Fatalf("PrepareSubmit error: %v", err)
			}
			if ojson.GetBytes(req.Body, "duration").Int() != tc.wantDuration {
				t.Errorf("duration = %d, want %d", ojson.GetBytes(req.Body, "duration").Int(), tc.wantDuration)
			}
			if ojson.GetBytes(req.Body, "ratio").String() != tc.wantRatio {
				t.Errorf("ratio = %q, want %q", ojson.GetBytes(req.Body, "ratio").String(), tc.wantRatio)
			}
			if ojson.GetBytes(req.Body, "resolution").String() != tc.wantResolution {
				t.Errorf("resolution = %q, want %q", ojson.GetBytes(req.Body, "resolution").String(), tc.wantResolution)
			}
		})
	}
}

func TestDriver_VolcengineSeedanceFlow_WithCanonicalMetadata(t *testing.T) {
	ctx := context.Background()
	driver := aigcdriver.NewDriver()

	input := aigc.GenerationSubmitInput{
		Generation: aigc.ContentGeneration{
			Kind:     aigc.ContentKindVideo,
			Model:    "volcengine/doubao-seedance-2-0-mini",
			Provider: "volcengine-seedance",
			Input:    []byte(`{"prompt":"landscape video","size":"854x480","seconds":9}`),
			Metadata: map[string]any{
				"canonical": map[string]any{
					"resolution": "480p",
					"ratio":      "16:9",
					"duration":   9,
					"pixels":     409920,
					"width":      854,
					"height":     480,
				},
			},
		},
	}

	req, err := driver.PrepareSubmit(ctx, input)
	if err != nil {
		t.Fatalf("PrepareSubmit error: %v", err)
	}
	if ojson.GetBytes(req.Body, "resolution").String() != "480p" {
		t.Errorf("resolution = %q, want 480p", ojson.GetBytes(req.Body, "resolution").String())
	}
	if ojson.GetBytes(req.Body, "ratio").String() != "16:9" {
		t.Errorf("ratio = %q, want 16:9", ojson.GetBytes(req.Body, "ratio").String())
	}
	if ojson.GetBytes(req.Body, "duration").Int() != 9 {
		t.Errorf("duration = %d, want 9", ojson.GetBytes(req.Body, "duration").Int())
	}
}

func TestDriver_SeedancePollFailure(t *testing.T) {
	ctx := context.Background()
	driver := aigcdriver.NewDriver()

	failedPollResp := aigc.GenerationExecutionResponse{
		StatusCode: 200,
		Metadata:   map[string]any{"provider": "volcengine-seedance", "task_id": "cgt-20260825102931-gd2rv"},
		Body: []byte(`{
			"id": "cgt-20260825102931-gd2rv",
			"model": "doubao-seedance-2-0-mini-260615",
			"status": "failed",
			"error": {
				"code": "OutputVideoSensitiveContentDetected.PolicyViolation",
				"message": "The request failed because the output video may be related to copyright restrictions."
			}
		}`),
	}

	pollRes, err := driver.ParsePoll(ctx, failedPollResp)
	if err != nil {
		t.Fatalf("ParsePoll failed error: %v", err)
	}
	if pollRes.Status != aigc.StatusFailed {
		t.Fatalf("Status = %v, want %v", pollRes.Status, aigc.StatusFailed)
	}
	if pollRes.Stage != aigc.StageFailed {
		t.Fatalf("Stage = %v, want %v", pollRes.Stage, aigc.StageFailed)
	}
	if pollRes.ErrorCode != aigc.ErrCodeCopyrightViolation {
		t.Fatalf("ErrorCode = %q, want %q", pollRes.ErrorCode, aigc.ErrCodeCopyrightViolation)
	}
	if pollRes.ErrorMessage == "" {
		t.Fatalf("ErrorMessage is empty")
	}
}
