package aigc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	log "github.com/sirupsen/logrus"
)

// ImageExecutor defines the interface for executing image model requests with AuthManager.
type ImageExecutor interface {
	ExecuteImageWithAuthManager(ctx context.Context, handlerType, modelName string, rawJSON []byte, alt string) ([]byte, http.Header, *interfaces.ErrorMessage)
}

// SyncImageRequest contains parameters for synchronous image generation/editing.
type SyncImageRequest struct {
	ID            string         `json:"id,omitempty"`
	RequestID     string         `json:"request_id,omitempty"`
	UserID        uint64         `json:"user_id,omitempty"`
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
	// Capture the server-selected operation before policy hooks can replace metadata.
	imageOperation, _ := req.Metadata["image_operation"].(string)
	genID := strings.TrimSpace(req.ID)
	if genID == "" {
		genID = "cg_" + strings.ReplaceAll(uuid.New().String(), "-", "")
	}

	model := strings.TrimSpace(req.Model)
	if model == "" && len(req.Input) > 0 {
		model = ojson.GetBytes(req.Input, "model").String()
	}

	draft := aigc.ContentGenerationDraft{
		ID:        genID,
		RequestID: req.RequestID,
		UserID:    req.UserID,
		Kind:      aigc.ContentKindImage,
		Model:     model,
		APIKey:    req.APIKey,
		ClientIP:  req.ClientIP,
		Input:     req.Input,
		Metadata:  req.Metadata,
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

	if imageOperation != "" {
		if draft.Metadata == nil {
			draft.Metadata = make(map[string]any)
		}
		draft.Metadata["image_operation"] = imageOperation
	}

	// 2. Persist initial record in store if available
	recorder, createdGen := p.beginSyncGeneration(ctx, draft, genID)

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
			p.failStoreRecord(ctx, recorder, "MutationRejected", errMsg)
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
		p.failStoreRecord(ctx, recorder, aigc.ErrCodeDriverNotFound, fmt.Sprintf("no image generation driver available for model %q", model))
		return SyncImageResult{}, &interfaces.ErrorMessage{
			StatusCode: http.StatusBadRequest,
			Error:      fmt.Errorf("no image generation driver available for model %q", model),
		}
	}

	// 5. PrepareSubmit via driver
	if imageOperation != "" {
		if gen.Metadata == nil {
			gen.Metadata = make(map[string]any)
		}
		gen.Metadata["image_operation"] = imageOperation
	}
	execReq, errPrep := driver.PrepareSubmit(ctx, aigc.GenerationSubmitInput{
		Generation: gen,
	})
	if errPrep != nil {
		p.failStoreRecord(ctx, recorder, aigc.ErrCodeInvalidParameter, errPrep.Error())
		return SyncImageResult{}, &interfaces.ErrorMessage{
			StatusCode: http.StatusBadRequest,
			Error:      fmt.Errorf("failed to prepare image request: %w", errPrep),
		}
	}

	// 6. Execute via AuthManager
	var respBody []byte
	var respHeaders http.Header
	var execErr *interfaces.ErrorMessage

	startSubmit := time.Now()
	if p.imageExecutor != nil {
		execCtx := handlers.WithRequestMetadata(ctx, map[string]any{
			cliproxyexecutor.CustomEndpointMetadataKey: execReq.URL,
			cliproxyexecutor.RequestPathMetadataKey:    execReq.URL,
			cliproxyexecutor.HTTPMethodMetadataKey:     execReq.Method,
		})
		if len(execReq.Header) > 0 {
			execCtx = handlers.WithRequestHeaders(execCtx, execReq.Header)
		}
		respBody, respHeaders, execErr = p.imageExecutor.ExecuteImageWithAuthManager(execCtx, "openai", model, execReq.Body, "")
	} else {
		execErr = &interfaces.ErrorMessage{
			StatusCode: http.StatusServiceUnavailable,
			Error:      fmt.Errorf("no image executor configured"),
		}
	}
	submitDurationMs := time.Since(startSubmit).Milliseconds()

	// The provider.submitted event records what was sent upstream and what came
	// back. It is buffered first and flushed with the terminal state, so a run that
	// never reaches the store cannot leave an orphan event behind.
	providerEvent := aigc.ContentGenerationEvent{
		GenerationID:    genID,
		EventType:       aigc.EventProviderSubmitted,
		ProviderRequest: syncEventPayload(execReq.Body),
		DurationMs:      submitDurationMs,
	}

	if execErr != nil {
		norm := aigc.NormalizeError(driverPluginID, execErr.StatusCode, execErr.Body, execErr.Error)
		upstreamStatus := execErr.StatusCode
		if upstreamStatus <= 0 {
			upstreamStatus = http.StatusBadGateway
		}
		providerEvent.Stage = aigc.StageFailed
		providerEvent.Status = aigc.StatusFailed
		providerEvent.ProviderResponse = syncEventPayload(execErr.Body)
		providerEvent.HTTPStatus = upstreamStatus
		recorder.record(providerEvent)
		p.failStoreRecord(ctx, recorder, norm.Code, norm.Message)
		return SyncImageResult{}, execErr
	}

	// 7. Parse submit response via driver
	submitResult, errParse := driver.ParseSubmit(ctx, aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Header:     respHeaders,
		Body:       respBody,
		Metadata:   execReq.Metadata,
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
		providerEvent.Stage = aigc.StageFailed
		providerEvent.Status = aigc.StatusFailed
		providerEvent.ProviderResponse = syncEventPayload(respBody)
		providerEvent.HTTPStatus = http.StatusOK
		recorder.record(providerEvent)
		p.failStoreRecord(ctx, recorder, errCode, errMsg)
		return SyncImageResult{}, &interfaces.ErrorMessage{
			StatusCode: http.StatusBadGateway,
			Error:      fmt.Errorf("%s", errMsg),
		}
	}

	// 8. PhaseBeforeComplete (Egress TOS upload for Base64 artifacts)
	finalArtifacts := submitResult.Artifacts
	finalOutput := submitResult.Output
	finalMetadata := draft.Metadata
	if p.pluginHost != nil && (len(finalArtifacts) > 0 || len(finalOutput) > 0) {
		persistGen := gen
		persistGen.Artifacts = finalArtifacts
		persistGen.Output = finalOutput
		persistGen.Metadata = finalMetadata
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
		if mutResp.Generation != nil {
			if len(mutResp.Generation.Output) > 0 {
				finalOutput = mutResp.Generation.Output
			}
			if len(mutResp.Generation.Metadata) > 0 {
				finalMetadata = mutResp.Generation.Metadata
			}
		}
	}

	// 9. Persist Succeeded status in store
	if recorder.enabled() {
		patchSet := map[string]any{
			"status":           aigc.StatusSucceeded,
			"stage":            aigc.StageCompleted,
			"progress":         100,
			"provider":         driverPluginID,
			"provider_task_id": submitResult.ProviderTaskID,
			"output":           finalOutput,
			"completed_at":     time.Now(),
			"duration_ms":      submitDurationMs,
		}
		if len(finalMetadata) > 0 {
			patchSet["metadata"] = finalMetadata
		}
		if usage := submitResult.Usage; usage != nil {
			patchSet["input_tokens"] = usage.InputTokens
			patchSet["output_tokens"] = usage.OutputTokens
			patchSet["total_tokens"] = usage.TotalTokens
		} else {
			patchSet["input_tokens"] = int64(0)
			patchSet["output_tokens"] = int64(0)
			patchSet["total_tokens"] = int64(0)
		}

		// The upstream call is complete before PhaseBeforeComplete rewrites the
		// artifacts; the event is appended in provider order after generation.created.
		providerEvent.Stage = aigc.StageCompleted
		providerEvent.Status = aigc.StatusSucceeded
		providerEvent.ProviderResponse = syncEventPayload(respBody)
		providerEvent.HTTPStatus = http.StatusOK
		recorder.record(providerEvent)
		recorder.record(aigc.ContentGenerationEvent{
			GenerationID: genID,
			EventType:    aigc.EventGenerationSucceeded,
			Stage:        aigc.StageCompleted,
			Status:       aigc.StatusSucceeded,
		})
		if errFlush := recorder.flush(ctx, patchSet, finalArtifacts); errFlush != nil {
			log.Warnf("aigc: failed to persist synchronous generation %s: %v", genID, errFlush)
		}
	}

	// 10. PhaseOnSucceeded (Billing settlement, audit hooks)
	if p.pluginHost != nil {
		gen.Status = aigc.StatusSucceeded
		gen.Stage = aigc.StageCompleted
		gen.Progress = 100
		gen.Artifacts = finalArtifacts
		gen.Output = finalOutput
		gen.Metadata = finalMetadata
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

// beginSyncGeneration creates the store record for a synchronous run and opens the
// event recorder that will carry its lifecycle events.
func (p *SyncPipeline) beginSyncGeneration(ctx context.Context, draft aigc.ContentGenerationDraft, genID string) (*syncGenerationRecorder, aigc.ContentGeneration) {
	if p.pluginHost == nil {
		return nil, aigc.ContentGeneration{}
	}
	store, _, okStore := p.pluginHost.ContentGenerationStore()
	if !okStore || store == nil {
		return nil, aigc.ContentGeneration{}
	}
	created, errCreate := store.Create(ctx, aigc.GenerationCreateRequest{Draft: draft})
	if errCreate != nil {
		log.Warnf("aigc: failed to create synchronous generation %s: %v", genID, errCreate)
		return nil, aigc.ContentGeneration{}
	}

	// Mark the record as submitting before the upstream call. Recovery claims any
	// accepted generation that is not submitting, so leaving it at created would let
	// the worker re-submit a request this process is already executing.
	claimed, errClaim := store.Patch(ctx, aigc.GenerationPatchRequest{
		ID:               created.ID,
		ExpectedRevision: created.Revision,
		Set:              map[string]any{"stage": aigc.StageSubmitting},
	})
	if errClaim != nil {
		log.Warnf("aigc: failed to claim synchronous generation %s: %v", genID, errClaim)
	} else {
		created = claimed
	}

	recorder := newSyncGenerationRecorder(store, created, p.pluginHost.EmitContentGenerationEvent)
	recorder.record(aigc.ContentGenerationEvent{
		GenerationID: genID,
		EventType:    aigc.EventGenerationCreated,
		Stage:        aigc.StageCreated,
		Status:       aigc.StatusAccepted,
	})
	return recorder, created
}

func (p *SyncPipeline) failStoreRecord(ctx context.Context, recorder *syncGenerationRecorder, code, msg string) {
	if !recorder.enabled() {
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

	recorder.record(aigc.ContentGenerationEvent{
		GenerationID: recorder.created.ID,
		EventType:    aigc.EventGenerationFailed,
		Stage:        aigc.StageFailed,
		Status:       aigc.StatusFailed,
	})
	if errFlush := recorder.flush(ctx, map[string]any{
		"status":        aigc.StatusFailed,
		"stage":         aigc.StageFailed,
		"error_code":    finalCode,
		"error_message": finalMsg,
		"completed_at":  time.Now(),
	}, nil); errFlush != nil {
		log.Warnf("aigc: failed to persist synchronous failure %s: %v", recorder.created.ID, errFlush)
	}
}
