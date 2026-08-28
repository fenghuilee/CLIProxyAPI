package aigc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/aigc"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	"github.com/tidwall/gjson"
)

// ImageExecutor defines the interface for executing image model requests with AuthManager.
type ImageExecutor interface {
	ExecuteImageWithAuthManager(ctx context.Context, handlerType, modelName string, rawJSON []byte, alt string) ([]byte, http.Header, *interfaces.ErrorMessage)
}

// SyncImageRequest contains parameters for synchronous image generation/editing.
type SyncImageRequest struct {
	ID            string         `json:"id,omitempty"`
	Model         string         `json:"model"`
	Input         []byte         `json:"input"`
	DesiredFormat string         `json:"desired_format,omitempty"` // "url" or "b64_json"
	APIKey        string         `json:"api_key,omitempty"`
	ClientIP      string         `json:"client_ip,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// SyncImageResult contains the outcome of a synchronous image generation pipeline.
type SyncImageResult struct {
	ID        string                           `json:"id"`
	Model     string                           `json:"model"`
	Artifacts []aigc.ContentGenerationArtifact `json:"artifacts"`
	RawOutput json.RawMessage                  `json:"raw_output,omitempty"`
	RawBody   []byte                           `json:"raw_body,omitempty"`
}

// SyncPipeline executes synchronous, direct request-response image pipelines.
type SyncPipeline struct {
	pluginHost    *pluginhost.Host
	imageExecutor ImageExecutor
	baseHandler   *handlers.BaseAPIHandler
}

// NewSyncPipeline creates a new synchronous image pipeline runner.
func NewSyncPipeline(host *pluginhost.Host, baseHandler *handlers.BaseAPIHandler) *SyncPipeline {
	var exec ImageExecutor
	if baseHandler != nil {
		exec = baseHandler
	}
	return &SyncPipeline{
		pluginHost:    host,
		imageExecutor: exec,
		baseHandler:   baseHandler,
	}
}

// SetImageExecutor overrides the image executor (useful for unit testing).
func (p *SyncPipeline) SetImageExecutor(exec ImageExecutor) {
	p.imageExecutor = exec
}

// Execute runs the complete synchronous image generation pipeline.
func (p *SyncPipeline) Execute(ctx context.Context, req SyncImageRequest) (SyncImageResult, *interfaces.ErrorMessage) {
	genID := strings.TrimSpace(req.ID)
	if genID == "" {
		genID = "cg_" + strings.ReplaceAll(uuid.New().String(), "-", "")
	}

	model := strings.TrimSpace(req.Model)
	if model == "" && len(req.Input) > 0 {
		model = gjson.GetBytes(req.Input, "model").String()
	}

	draft := aigc.ContentGenerationDraft{
		ID:       genID,
		Kind:     aigc.ContentKindImage,
		Model:    model,
		APIKey:   req.APIKey,
		ClientIP: req.ClientIP,
		Input:    req.Input,
		Metadata: req.Metadata,
	}

	// 1. PhaseBeforeCreate (Ingress transformation, Data-URI to TOS, Asset pre-creation)
	if p.pluginHost != nil {
		mutResp, errMutate := p.pluginHost.MutateContentGeneration(ctx, aigc.GenerationMutationRequest{
			Phase: aigc.PhaseBeforeCreate,
			Draft: &draft,
		})
		if errMutate != nil {
			return SyncImageResult{}, &interfaces.ErrorMessage{
				StatusCode: http.StatusBadRequest,
				Error:      fmt.Errorf("before create mutation: %w", errMutate),
			}
		}
		if mutResp.Reject {
			errMsg := mutResp.ErrorMessage
			if errMsg == "" {
				errMsg = "rejected by before create policy"
			}
			return SyncImageResult{}, &interfaces.ErrorMessage{
				StatusCode: http.StatusBadRequest,
				Error:      fmt.Errorf("%s (%s)", errMsg, mutResp.ErrorCode),
			}
		}
		if mutResp.Draft != nil {
			draft = *mutResp.Draft
			if len(draft.Input) > 0 {
				req.Input = draft.Input
			}
			if draft.Model != "" {
				model = draft.Model
			}
		}
	}

	// 2. Persist initial record in store if available
	var store aigc.ContentGenerationStore
	var createdGen aigc.ContentGeneration
	var okStore bool
	if p.pluginHost != nil {
		store, _, okStore = p.pluginHost.ContentGenerationStore()
		if okStore && store != nil {
			var errCreate error
			createdGen, errCreate = store.Create(ctx, aigc.GenerationCreateRequest{Draft: draft})
			if errCreate == nil {
				p.pluginHost.EmitContentGenerationEvent(ctx, aigc.ContentGenerationEvent{
					GenerationID: genID,
					Revision:     createdGen.Revision,
					EventType:    aigc.EventGenerationCreated,
					Stage:        aigc.StageCreated,
					Status:       aigc.StatusAccepted,
					CreatedAt:    time.Now(),
				})
			}
		}
	}

	gen := aigc.ContentGeneration{
		ID:        genID,
		Kind:      aigc.ContentKindImage,
		Model:     model,
		Input:     req.Input,
		APIKey:    req.APIKey,
		ClientIP:  req.ClientIP,
		Metadata:  draft.Metadata,
		Revision:  createdGen.Revision,
		CreatedAt: time.Now(),
	}

	// 3. PhaseBeforeSubmit (Readiness checks, prompt rewrite, asset wait)
	if p.pluginHost != nil {
		mutRespSubmit, errMutateSubmit := p.pluginHost.MutateContentGeneration(ctx, aigc.GenerationMutationRequest{
			Phase:      aigc.PhaseBeforeSubmit,
			Generation: gen,
		})
		if errMutateSubmit != nil || mutRespSubmit.Reject {
			errMsg := "submission rejected by policy"
			if mutRespSubmit.ErrorMessage != "" {
				errMsg = mutRespSubmit.ErrorMessage
			}
			if errMutateSubmit != nil {
				errMsg = errMutateSubmit.Error()
			}
			p.failStoreRecord(ctx, store, createdGen, "MutationRejected", errMsg)
			return SyncImageResult{}, &interfaces.ErrorMessage{
				StatusCode: http.StatusBadRequest,
				Error:      fmt.Errorf("%s", errMsg),
			}
		}
		if mutRespSubmit.Generation != nil {
			gen = *mutRespSubmit.Generation
			if len(gen.Input) > 0 {
				req.Input = gen.Input
			}
		}
	}

	// 4. Find driver
	if p.pluginHost == nil {
		return SyncImageResult{}, &interfaces.ErrorMessage{
			StatusCode: http.StatusServiceUnavailable,
			Error:      fmt.Errorf("plugin host unavailable"),
		}
	}
	driver, driverPluginID, okDriver := p.pluginHost.ContentGenerationDriverFor(ctx, aigc.ContentKindImage, model)
	if !okDriver || driver == nil {
		p.failStoreRecord(ctx, store, createdGen, aigc.ErrCodeDriverNotFound, fmt.Sprintf("no image generation driver available for model %q", model))
		return SyncImageResult{}, &interfaces.ErrorMessage{
			StatusCode: http.StatusBadRequest,
			Error:      fmt.Errorf("no image generation driver available for model %q", model),
		}
	}

	// 5. PrepareSubmit via driver
	execReq, errPrep := driver.PrepareSubmit(ctx, aigc.GenerationSubmitInput{
		Generation: gen,
	})
	if errPrep != nil {
		p.failStoreRecord(ctx, store, createdGen, aigc.ErrCodeInvalidParameter, errPrep.Error())
		return SyncImageResult{}, &interfaces.ErrorMessage{
			StatusCode: http.StatusBadRequest,
			Error:      fmt.Errorf("failed to prepare image request: %w", errPrep),
		}
	}

	// 6. Execute via AuthManager
	var respBody []byte
	var respHeaders http.Header
	var execErr *interfaces.ErrorMessage

	if p.imageExecutor != nil {
		execCtx := handlers.WithRequestMetadata(ctx, map[string]any{
			cliproxyexecutor.CustomEndpointMetadataKey: execReq.URL,
			cliproxyexecutor.RequestPathMetadataKey:    execReq.URL,
			cliproxyexecutor.HTTPMethodMetadataKey:     execReq.Method,
		})
		respBody, respHeaders, execErr = p.imageExecutor.ExecuteImageWithAuthManager(execCtx, "openai", model, execReq.Body, "")
	} else {
		execErr = &interfaces.ErrorMessage{
			StatusCode: http.StatusServiceUnavailable,
			Error:      fmt.Errorf("no image executor configured"),
		}
	}

	if execErr != nil {
		norm := aigc.NormalizeError(driverPluginID, execErr.StatusCode, execErr.Body, execErr.Error)
		p.failStoreRecord(ctx, store, createdGen, norm.Code, norm.Message)
		return SyncImageResult{}, execErr
	}

	// 7. Parse submit response via driver
	submitResult, errParse := driver.ParseSubmit(ctx, aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Header:     respHeaders,
		Body:       respBody,
	})
	if errParse != nil || submitResult.Status == aigc.StatusFailed {
		errCode := submitResult.ErrorCode
		if errCode == "" {
			errCode = aigc.ErrCodeInternalServerError
		}
		errMsg := submitResult.ErrorMessage
		if errMsg == "" && errParse != nil {
			errMsg = fmt.Sprintf("failed to parse upstream image response: %v", errParse)
		}
		p.failStoreRecord(ctx, store, createdGen, errCode, errMsg)
		return SyncImageResult{}, &interfaces.ErrorMessage{
			StatusCode: http.StatusBadGateway,
			Error:      fmt.Errorf("%s", errMsg),
		}
	}

	// 8. PhaseBeforeComplete (Egress TOS upload for Base64 artifacts)
	finalArtifacts := submitResult.Artifacts
	finalOutput := submitResult.Output
	if p.pluginHost != nil && (len(finalArtifacts) > 0 || len(finalOutput) > 0) {
		persistGen := gen
		persistGen.Artifacts = finalArtifacts
		persistGen.Output = finalOutput
		mutResp, _ := p.pluginHost.MutateContentGeneration(ctx, aigc.GenerationMutationRequest{
			Phase:      aigc.PhaseBeforeComplete,
			Generation: persistGen,
			Metadata: map[string]any{
				"artifacts": finalArtifacts,
			},
		})
		if len(mutResp.Artifacts) > 0 {
			finalArtifacts = mutResp.Artifacts
		}
		if mutResp.Generation != nil && len(mutResp.Generation.Output) > 0 {
			finalOutput = mutResp.Generation.Output
		}
	}

	// 9. Persist Succeeded status in store
	if store != nil && okStore {
		_, _ = store.Patch(ctx, aigc.GenerationPatchRequest{
			ID:               genID,
			ExpectedRevision: createdGen.Revision,
			Set: map[string]any{
				"status":            aigc.StatusSucceeded,
				"stage":             aigc.StageCompleted,
				"progress":          100,
				"provider":          driverPluginID,
				"provider_request":  json.RawMessage(execReq.Body),
				"provider_task_id":  submitResult.ProviderTaskID,
				"provider_response": aigc.SanitizeProviderResponse(submitResult.ProviderResponse),
				"output":            finalOutput,
			},
			Artifacts: finalArtifacts,
		})
		p.pluginHost.EmitContentGenerationEvent(ctx, aigc.ContentGenerationEvent{
			GenerationID: genID,
			EventType:    aigc.EventGenerationSucceeded,
			Stage:        aigc.StageCompleted,
			Status:       aigc.StatusSucceeded,
			CreatedAt:    time.Now(),
		})
	}

	// 10. PhaseOnSucceeded (Billing settlement, audit hooks)
	if p.pluginHost != nil {
		gen.Status = aigc.StatusSucceeded
		gen.Stage = aigc.StageCompleted
		gen.Progress = 100
		gen.Artifacts = finalArtifacts
		gen.Output = finalOutput
		_, _ = p.pluginHost.MutateContentGeneration(ctx, aigc.GenerationMutationRequest{
			Phase:      aigc.PhaseOnSucceeded,
			Generation: gen,
		})
	}

	return SyncImageResult{
		ID:        genID,
		Model:     model,
		Artifacts: finalArtifacts,
		RawOutput: finalOutput,
		RawBody:   respBody,
	}, nil
}

func (p *SyncPipeline) failStoreRecord(ctx context.Context, store aigc.ContentGenerationStore, createdGen aigc.ContentGeneration, code, msg string) {
	if store == nil || createdGen.ID == "" {
		return
	}
	norm := aigc.NormalizeError("", 500, []byte(msg), fmt.Errorf("%s", msg))
	finalCode := norm.Code
	if code != "" && code != "UPSTREAM_ERROR" && code != "PARSE_ERROR" && code != "PREPARE_SUBMIT_FAILED" && code != "DRIVER_NOT_FOUND" {
		finalCode = code
	}
	finalMsg := norm.Message
	if finalMsg == "" {
		finalMsg = msg
	}

	_, _ = store.Patch(ctx, aigc.GenerationPatchRequest{
		ID:               createdGen.ID,
		ExpectedRevision: createdGen.Revision,
		Set: map[string]any{
			"status":        aigc.StatusFailed,
			"stage":         aigc.StageFailed,
			"error_code":    finalCode,
			"error_message": finalMsg,
		},
	})
	if p.pluginHost != nil {
		p.pluginHost.EmitContentGenerationEvent(ctx, aigc.ContentGenerationEvent{
			GenerationID: createdGen.ID,
			EventType:    aigc.EventGenerationFailed,
			Stage:        aigc.StageFailed,
			Status:       aigc.StatusFailed,
			CreatedAt:    time.Now(),
		})
	}
}
