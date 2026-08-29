package aigc

import (
	"context"
	"encoding/json"
	"errors"
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
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

// Executor represents the core execution engine that handles ModelRouter, AuthManager, and Provider execution.
type Executor interface {
	ExecuteWithAuthManager(ctx context.Context, handlerType, modelName string, rawJSON []byte, alt string) ([]byte, http.Header, *interfaces.ErrorMessage)
}

// Coordinator coordinates asynchronous content generation lifecycles.
type Coordinator struct {
	pluginHost   *pluginhost.Host
	executor     Executor
	baseHandler  *handlers.BaseAPIHandler
	pollInterval time.Duration
}

// NewCoordinator creates a new AIGC lifecycle coordinator.
func NewCoordinator(host *pluginhost.Host, baseHandler *handlers.BaseAPIHandler) *Coordinator {
	var exec Executor
	if baseHandler != nil {
		exec = baseHandler
	}
	return &Coordinator{
		pluginHost:  host,
		executor:    exec,
		baseHandler: baseHandler,
	}
}

// SetExecutor overrides the executor (useful for testing).
func (c *Coordinator) SetExecutor(exec Executor) {
	c.executor = exec
}

// SetPollInterval sets the background polling interval (useful for testing).
func (c *Coordinator) SetPollInterval(interval time.Duration) {
	c.pollInterval = interval
}

// CreateGeneration creates and initiates an asynchronous content generation task.
func (c *Coordinator) CreateGeneration(ctx context.Context, kind aigc.ContentKind, draft aigc.ContentGenerationDraft) (aigc.ContentGeneration, error) {
	if draft.ID == "" {
		draft.ID = "cg_" + strings.ReplaceAll(uuid.New().String(), "-", "")
	}
	draft.Kind = kind
	if draft.Model == "" && len(draft.Input) > 0 {
		draft.Model = gjson.GetBytes(draft.Input, "model").String()
	}

	// 1. BeforeCreate mutators
	mutResp, errMutate := c.pluginHost.MutateContentGeneration(ctx, aigc.GenerationMutationRequest{
		Phase: aigc.PhaseBeforeCreate,
		Draft: &draft,
	})
	if errMutate != nil {
		return aigc.ContentGeneration{}, fmt.Errorf("before create mutation: %w", errMutate)
	}
	if mutResp.Reject {
		return aigc.ContentGeneration{}, fmt.Errorf("%w: %s (%s)", aigc.ErrMutationRejected, mutResp.ErrorMessage, mutResp.ErrorCode)
	}
	if mutResp.Draft != nil {
		draft = *mutResp.Draft
	}

	// 2. Obtain Store Plugin
	store, _, okStore := c.pluginHost.ContentGenerationStore()
	if !okStore || store == nil {
		return aigc.ContentGeneration{}, aigc.ErrStoreNotConfigured
	}

	// 3. Create initial Generation in store
	createdGen, errCreate := store.Create(ctx, aigc.GenerationCreateRequest{
		Draft: draft,
	})
	if errCreate != nil {
		return aigc.ContentGeneration{}, fmt.Errorf("store create generation: %w", errCreate)
	}

	// 4. Emit GenerationCreated event
	c.pluginHost.EmitContentGenerationEvent(ctx, aigc.ContentGenerationEvent{
		GenerationID: createdGen.ID,
		Revision:     createdGen.Revision,
		EventType:    aigc.EventGenerationCreated,
		Stage:        createdGen.Stage,
		Status:       createdGen.Status,
		CreatedAt:    time.Now(),
	})

	// 5. Asynchronously trigger submit pipeline
	go c.AsyncSubmit(createdGen.ID)

	return createdGen, nil
}

// AsyncSubmit runs the submit pipeline in background for a newly accepted generation.
func (c *Coordinator) AsyncSubmit(id string) {
	ctx := context.Background()
	store, _, okStore := c.pluginHost.ContentGenerationStore()
	if !okStore || store == nil {
		log.Errorf("aigc: async submit for %s failed: store not configured", id)
		return
	}

	gen, errGet := store.Get(ctx, aigc.GenerationGetRequest{ID: id})
	if errGet != nil {
		log.Errorf("aigc: async submit for %s failed to get gen: %v", id, errGet)
		return
	}

	if gen.Status != aigc.StatusAccepted {
		return
	}

	// 1. Mutate BeforeSubmit (readiness checks, asset activation, payload modifications)
	mutRespSubmit, errMutateSubmit := c.pluginHost.MutateContentGeneration(ctx, aigc.GenerationMutationRequest{
		Phase:      aigc.PhaseBeforeSubmit,
		Generation: gen,
	})
	if errMutateSubmit != nil || mutRespSubmit.Reject {
		errCode := aigc.ErrCodeMutationRejected
		errMsg := "submission rejected by policy"
		if mutRespSubmit.ErrorCode != "" {
			errCode = mutRespSubmit.ErrorCode
		}
		if mutRespSubmit.ErrorMessage != "" {
			errMsg = mutRespSubmit.ErrorMessage
		}
		if errMutateSubmit != nil {
			errMsg = errMutateSubmit.Error()
		}
		c.failGeneration(ctx, gen, errCode, errMsg)
		return
	}
	if mutRespSubmit.Generation != nil {
		gen = *mutRespSubmit.Generation
	}

	// 2. Find driver
	driver, driverPluginID, okDriver := c.pluginHost.ContentGenerationDriverFor(ctx, gen.Kind, gen.Model)
	if !okDriver || driver == nil {
		c.failGeneration(ctx, gen, aigc.ErrCodeDriverNotFound, fmt.Sprintf("no driver available for kind=%s model=%s", gen.Kind, gen.Model))
		return
	}

	// 3. PrepareSubmit via driver
	execReq, errPrep := driver.PrepareSubmit(ctx, aigc.GenerationSubmitInput{Generation: gen})
	if errPrep != nil {
		c.failGeneration(ctx, gen, aigc.ErrCodeInvalidParameter, errPrep.Error())
		return
	}

	// 4. Execute via CPA AuthManager
	var respBody []byte
	var respHeaders http.Header
	var execErr *interfaces.ErrorMessage

	if c.executor != nil {
		execCtx := handlers.WithRequestMetadata(ctx, map[string]any{
			cliproxyexecutor.CustomEndpointMetadataKey: execReq.URL,
			cliproxyexecutor.RequestPathMetadataKey:    execReq.URL,
			cliproxyexecutor.HTTPMethodMetadataKey:     execReq.Method,
		})
		respBody, respHeaders, execErr = c.executor.ExecuteWithAuthManager(execCtx, "openai", gen.Model, execReq.Body, "")
	} else {
		execErr = &interfaces.ErrorMessage{Error: fmt.Errorf("no executor configured")}
	}

	if execErr != nil {
		norm := aigc.NormalizeError(driverPluginID, execErr.StatusCode, execErr.Body, execErr.Error)
		c.failGeneration(ctx, gen, norm.Code, norm.Message)
		return
	}

	// 5. Parse submit response via driver
	submitResult, errParse := driver.ParseSubmit(ctx, aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Header:     respHeaders,
		Body:       respBody,
	})
	if errParse != nil {
		norm := aigc.NormalizeError(driverPluginID, http.StatusInternalServerError, respBody, errParse)
		c.failGeneration(ctx, gen, norm.Code, norm.Message)
		return
	}

	status := submitResult.Status
	if status == "" {
		status = aigc.StatusRunning
	}
	stage := submitResult.Stage
	if stage == "" {
		if status == aigc.StatusSucceeded {
			stage = aigc.StageCompleted
		} else {
			stage = aigc.StageProviderRunning
		}
	}

	patchSet := map[string]any{
		"status":           status,
		"stage":            stage,
		"progress":         submitResult.Progress,
		"provider":         driverPluginID,
		"provider_request": json.RawMessage(execReq.Body),
		"provider_task_id": submitResult.ProviderTaskID,
	}
	if len(submitResult.Output) > 0 {
		patchSet["output"] = submitResult.Output
	}
	if submitResult.ErrorCode != "" {
		patchSet["error_code"] = submitResult.ErrorCode
		patchSet["error_message"] = submitResult.ErrorMessage
	}

	artifactsToPersist := submitResult.Artifacts
	if len(artifactsToPersist) > 0 || len(submitResult.Output) > 0 {
		persistGen := gen
		persistGen.Artifacts = submitResult.Artifacts
		persistGen.Output = submitResult.Output
		mutResp, _ := c.pluginHost.MutateContentGeneration(ctx, aigc.GenerationMutationRequest{
			Phase:      aigc.PhaseBeforeComplete,
			Generation: persistGen,
			Metadata: map[string]any{
				"artifacts": submitResult.Artifacts,
			},
		})
		if len(mutResp.Artifacts) > 0 {
			artifactsToPersist = mutResp.Artifacts
		}
		if mutResp.Generation != nil {
			if len(mutResp.Generation.Output) > 0 {
				patchSet["output"] = mutResp.Generation.Output
			}
			if len(mutResp.Generation.Metadata) > 0 {
				patchSet["metadata"] = mutResp.Generation.Metadata
			}
		}
	}

	patchGen, errPatch := store.Patch(ctx, aigc.GenerationPatchRequest{
		ID:               gen.ID,
		ExpectedRevision: gen.Revision,
		Set:              patchSet,
		Artifacts:        artifactsToPersist,
	})
	if errPatch != nil {
		log.Warnf("aigc: failed to patch submit result for %s: %v", gen.ID, errPatch)
		return
	}
	gen = patchGen

	c.pluginHost.EmitContentGenerationEvent(ctx, aigc.ContentGenerationEvent{
		GenerationID: gen.ID,
		Revision:     gen.Revision,
		EventType:    aigc.EventProviderSubmitted,
		Stage:        gen.Stage,
		Status:       gen.Status,
		CreatedAt:    time.Now(),
	})

	if gen.Status == aigc.StatusSucceeded {
		c.pluginHost.EmitContentGenerationEvent(ctx, aigc.ContentGenerationEvent{
			GenerationID: gen.ID,
			Revision:     gen.Revision,
			EventType:    aigc.EventGenerationSucceeded,
			Stage:        gen.Stage,
			Status:       gen.Status,
			CreatedAt:    time.Now(),
		})
		_, _ = c.pluginHost.MutateContentGeneration(ctx, aigc.GenerationMutationRequest{
			Phase:      aigc.PhaseOnSucceeded,
			Generation: gen,
		})
	} else if gen.Status == aigc.StatusFailed {
		c.pluginHost.EmitContentGenerationEvent(ctx, aigc.ContentGenerationEvent{
			GenerationID: gen.ID,
			Revision:     gen.Revision,
			EventType:    aigc.EventGenerationFailed,
			Stage:        gen.Stage,
			Status:       gen.Status,
			CreatedAt:    time.Now(),
		})
		_, _ = c.pluginHost.MutateContentGeneration(ctx, aigc.GenerationMutationRequest{
			Phase:      aigc.PhaseOnFailed,
			Generation: gen,
		})
	}

	// Start active background polling until completion
	if gen.Status == aigc.StatusRunning && gen.ProviderTaskID != "" && driver != nil {
		go func() {
			pollCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			c.pollUntilComplete(pollCtx, gen.ID, driver)
		}()
	}
}

// pollUntilComplete periodically polls the provider in the background until the generation reaches a terminal state.
func (c *Coordinator) pollUntilComplete(ctx context.Context, id string, driver aigc.ContentGenerationDriver) {
	store, _, okStore := c.pluginHost.ContentGenerationStore()
	if !okStore || store == nil {
		return
	}

	interval := c.pollInterval
	if interval <= 0 {
		interval = 3 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			gen, errGet := store.Get(ctx, aigc.GenerationGetRequest{ID: id})
			if errGet != nil || aigc.IsTerminalStatus(gen.Status) || gen.Status != aigc.StatusRunning || gen.ProviderTaskID == "" {
				return
			}
			polled := c.pollGeneration(ctx, store, driver, gen)
			if aigc.IsTerminalStatus(polled.Status) {
				return
			}
		}
	}
}

// GetGeneration retrieves generation details and performs on-demand polling if actively running.
func (c *Coordinator) GetGeneration(ctx context.Context, id string) (aigc.ContentGeneration, error) {
	store, _, okStore := c.pluginHost.ContentGenerationStore()
	if !okStore || store == nil {
		return aigc.ContentGeneration{}, aigc.ErrStoreNotConfigured
	}

	gen, errGet := store.Get(ctx, aigc.GenerationGetRequest{ID: id})
	if errGet != nil {
		return aigc.ContentGeneration{}, errGet
	}

	// Poll driver if running and provider_task_id exists
	if gen.Status == aigc.StatusRunning && gen.ProviderTaskID != "" {
		driver, _, okDriver := c.pluginHost.ContentGenerationDriverFor(ctx, gen.Kind, gen.Model)
		if okDriver && driver != nil {
			gen = c.pollGeneration(ctx, store, driver, gen)
		}
	}

	return gen, nil
}

func (c *Coordinator) pollGeneration(ctx context.Context, store aigc.ContentGenerationStore, driver aigc.ContentGenerationDriver, gen aigc.ContentGeneration) aigc.ContentGeneration {
	pollReq, errPrep := driver.PreparePoll(ctx, aigc.GenerationPollInput{Generation: gen})
	if errPrep != nil {
		log.Debugf("aigc: driver prepare poll failed for %s: %v", gen.ID, errPrep)
		return gen
	}

	var respBody []byte
	var respHeaders http.Header
	var execErr *interfaces.ErrorMessage

	if c.executor != nil {
		execCtx := handlers.WithRequestMetadata(ctx, map[string]any{
			cliproxyexecutor.CustomEndpointMetadataKey: pollReq.URL,
			cliproxyexecutor.RequestPathMetadataKey:    pollReq.URL,
			cliproxyexecutor.HTTPMethodMetadataKey:     pollReq.Method,
		})
		respBody, respHeaders, execErr = c.executor.ExecuteWithAuthManager(execCtx, "openai", gen.Model, pollReq.Body, "")
	}

	if execErr != nil {
		log.Warnf("aigc: poll execution failed for %s: %v", gen.ID, execErr.Error)
		return gen
	}

	pollResult, errParse := driver.ParsePoll(ctx, aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Header:     respHeaders,
		Body:       respBody,
	})
	if errParse != nil {
		log.Warnf("aigc: driver parse poll failed for %s: %v", gen.ID, errParse)
		return gen
	}

	patchSet := map[string]any{
		"progress": pollResult.Progress,
	}
	if pollResult.Status != "" && pollResult.Status != gen.Status {
		patchSet["status"] = pollResult.Status
	}
	if pollResult.Stage != "" && pollResult.Stage != gen.Stage {
		patchSet["stage"] = pollResult.Stage
	}
	if len(pollResult.Output) > 0 {
		patchSet["output"] = pollResult.Output
	}
	if pollResult.ErrorCode != "" {
		patchSet["error_code"] = pollResult.ErrorCode
		patchSet["error_message"] = pollResult.ErrorMessage
	}

	artifactsToPersist := pollResult.Artifacts
	if len(artifactsToPersist) > 0 || len(pollResult.Output) > 0 {
		persistGen := gen
		persistGen.Artifacts = pollResult.Artifacts
		persistGen.Output = pollResult.Output
		mutResp, _ := c.pluginHost.MutateContentGeneration(ctx, aigc.GenerationMutationRequest{
			Phase:      aigc.PhaseBeforeComplete,
			Generation: persistGen,
			Metadata: map[string]any{
				"artifacts": pollResult.Artifacts,
			},
		})
		if len(mutResp.Artifacts) > 0 {
			artifactsToPersist = mutResp.Artifacts
		}
		if mutResp.Generation != nil {
			if len(mutResp.Generation.Output) > 0 {
				patchSet["output"] = mutResp.Generation.Output
			}
			if len(mutResp.Generation.Metadata) > 0 {
				patchSet["metadata"] = mutResp.Generation.Metadata
			}
		}
	}

	patchGen, errPatch := store.Patch(ctx, aigc.GenerationPatchRequest{
		ID:               gen.ID,
		ExpectedRevision: gen.Revision,
		Set:              patchSet,
		Artifacts:        artifactsToPersist,
	})
	if errPatch != nil {
		log.Debugf("aigc: failed to patch poll result for %s: %v", gen.ID, errPatch)
		return gen
	}
	gen = patchGen

	c.pluginHost.EmitContentGenerationEvent(ctx, aigc.ContentGenerationEvent{
		GenerationID: gen.ID,
		Revision:     gen.Revision,
		EventType:    aigc.EventProviderPolled,
		Stage:        gen.Stage,
		Status:       gen.Status,
		CreatedAt:    time.Now(),
	})

	if gen.Status == aigc.StatusSucceeded {
		c.pluginHost.EmitContentGenerationEvent(ctx, aigc.ContentGenerationEvent{
			GenerationID: gen.ID,
			Revision:     gen.Revision,
			EventType:    aigc.EventGenerationSucceeded,
			Stage:        gen.Stage,
			Status:       gen.Status,
			CreatedAt:    time.Now(),
		})
		_, _ = c.pluginHost.MutateContentGeneration(ctx, aigc.GenerationMutationRequest{
			Phase:      aigc.PhaseOnSucceeded,
			Generation: gen,
		})
	} else if gen.Status == aigc.StatusFailed {
		c.pluginHost.EmitContentGenerationEvent(ctx, aigc.ContentGenerationEvent{
			GenerationID: gen.ID,
			Revision:     gen.Revision,
			EventType:    aigc.EventGenerationFailed,
			Stage:        gen.Stage,
			Status:       gen.Status,
			CreatedAt:    time.Now(),
		})
		_, _ = c.pluginHost.MutateContentGeneration(ctx, aigc.GenerationMutationRequest{
			Phase:      aigc.PhaseOnFailed,
			Generation: gen,
		})
	}

	return gen
}

// CancelGeneration cancels an active content generation task.
func (c *Coordinator) CancelGeneration(ctx context.Context, id string) (aigc.ContentGeneration, error) {
	store, _, okStore := c.pluginHost.ContentGenerationStore()
	if !okStore || store == nil {
		return aigc.ContentGeneration{}, aigc.ErrStoreNotConfigured
	}

	gen, errGet := store.Get(ctx, aigc.GenerationGetRequest{ID: id})
	if errGet != nil {
		return aigc.ContentGeneration{}, errGet
	}

	if aigc.IsTerminalStatus(gen.Status) {
		if gen.Status == aigc.StatusCanceled {
			return gen, nil
		}
		return aigc.ContentGeneration{}, aigc.ErrGenerationCompleted
	}

	mutResp, errMutate := c.pluginHost.MutateContentGeneration(ctx, aigc.GenerationMutationRequest{
		Phase:      aigc.PhaseBeforeCancel,
		Generation: gen,
	})
	if errMutate != nil || mutResp.Reject {
		errMsg := "cancel rejected by policy"
		if mutResp.ErrorMessage != "" {
			errMsg = mutResp.ErrorMessage
		}
		if errMutate != nil {
			errMsg = errMutate.Error()
		}
		return aigc.ContentGeneration{}, fmt.Errorf("%w: %s", aigc.ErrMutationRejected, errMsg)
	}
	if mutResp.Generation != nil {
		gen = *mutResp.Generation
	}

	// Try upstream cancel if running
	if gen.Status == aigc.StatusRunning && gen.ProviderTaskID != "" {
		driver, _, okDriver := c.pluginHost.ContentGenerationDriverFor(ctx, gen.Kind, gen.Model)
		if okDriver && driver != nil {
			cancelReq, errPrep := driver.PrepareCancel(ctx, aigc.GenerationCancelInput{Generation: gen})
			if errPrep == nil && c.executor != nil {
				execCtx := handlers.WithRequestMetadata(ctx, map[string]any{
					cliproxyexecutor.CustomEndpointMetadataKey: cancelReq.URL,
					cliproxyexecutor.RequestPathMetadataKey:    cancelReq.URL,
					cliproxyexecutor.HTTPMethodMetadataKey:     cancelReq.Method,
				})
				respBody, respHeaders, execErr := c.executor.ExecuteWithAuthManager(execCtx, "openai", gen.Model, cancelReq.Body, "")
				if execErr == nil {
					_ = driver.ParseCancel(ctx, aigc.GenerationExecutionResponse{
						StatusCode: http.StatusOK,
						Header:     respHeaders,
						Body:       respBody,
					})
				}
			}
		}
	}

	patchGen, errPatch := store.Patch(ctx, aigc.GenerationPatchRequest{
		ID:               gen.ID,
		ExpectedRevision: gen.Revision,
		Set: map[string]any{
			"status": aigc.StatusCanceled,
			"stage":  aigc.StageCanceled,
		},
	})
	if errPatch != nil {
		return aigc.ContentGeneration{}, fmt.Errorf("cancel patch failed: %w", errPatch)
	}

	c.pluginHost.EmitContentGenerationEvent(ctx, aigc.ContentGenerationEvent{
		GenerationID: patchGen.ID,
		Revision:     patchGen.Revision,
		EventType:    aigc.EventGenerationCanceled,
		Stage:        patchGen.Stage,
		Status:       patchGen.Status,
		CreatedAt:    time.Now(),
	})

	_, _ = c.pluginHost.MutateContentGeneration(ctx, aigc.GenerationMutationRequest{
		Phase:      aigc.PhaseOnCanceled,
		Generation: patchGen,
	})

	return patchGen, nil
}

func (c *Coordinator) failGeneration(ctx context.Context, gen aigc.ContentGeneration, code, msg string) {
	if gen.ID == "" {
		return
	}
	store, _, okStore := c.pluginHost.ContentGenerationStore()
	if !okStore || store == nil {
		return
	}

	norm := aigc.NormalizeError("", 500, []byte(msg), errors.New(msg))
	finalCode := norm.Code
	if code != "" && code != "ProviderSubmitFailed" && code != "ParseSubmitFailed" && code != "PrepareSubmitFailed" && code != "DriverNotFound" {
		finalCode = code
	}
	finalMsg := norm.Message
	if finalMsg == "" {
		finalMsg = msg
	}

	patchGen, errPatch := store.Patch(ctx, aigc.GenerationPatchRequest{
		ID:               gen.ID,
		ExpectedRevision: gen.Revision,
		Set: map[string]any{
			"status":        aigc.StatusFailed,
			"stage":         aigc.StageFailed,
			"error_code":    finalCode,
			"error_message": finalMsg,
		},
	})
	if errPatch != nil {
		// Retry with latest revision in case of concurrent update
		if latest, errGet := store.Get(ctx, aigc.GenerationGetRequest{ID: gen.ID}); errGet == nil {
			patchGen, errPatch = store.Patch(ctx, aigc.GenerationPatchRequest{
				ID:               latest.ID,
				ExpectedRevision: latest.Revision,
				Set: map[string]any{
					"status":        aigc.StatusFailed,
					"stage":         aigc.StageFailed,
					"error_code":    finalCode,
					"error_message": finalMsg,
				},
			})
		}
	}
	if errPatch != nil {
		log.Warnf("aigc: failed to mark generation failed for %s: %v", gen.ID, errPatch)
		return
	}

	c.pluginHost.EmitContentGenerationEvent(ctx, aigc.ContentGenerationEvent{
		GenerationID: patchGen.ID,
		Revision:     patchGen.Revision,
		EventType:    aigc.EventGenerationFailed,
		Stage:        patchGen.Stage,
		Status:       patchGen.Status,
		CreatedAt:    time.Now(),
	})

	_, _ = c.pluginHost.MutateContentGeneration(ctx, aigc.GenerationMutationRequest{
		Phase:      aigc.PhaseOnFailed,
		Generation: patchGen,
	})
}
