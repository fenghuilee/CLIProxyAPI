package pluginhost

import (
	"context"
	"fmt"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/aigc"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	log "github.com/sirupsen/logrus"
)

type rpcContentGenerationStore struct {
	*rpcPluginAdapter
}

func (s rpcContentGenerationStore) Create(ctx context.Context, req aigc.GenerationCreateRequest) (aigc.ContentGeneration, error) {
	resp, errCall := callPlugin[aigc.ContentGeneration](ctx, s.client, pluginabi.MethodContentGenerationStoreCreate, rpcContentGenerationStoreCreateRequest{
		GenerationCreateRequest: req,
	})
	if errCall != nil {
		return aigc.ContentGeneration{}, errCall
	}
	return resp, nil
}

func (s rpcContentGenerationStore) Get(ctx context.Context, req aigc.GenerationGetRequest) (aigc.ContentGeneration, error) {
	resp, errCall := callPlugin[aigc.ContentGeneration](ctx, s.client, pluginabi.MethodContentGenerationStoreGet, rpcContentGenerationStoreGetRequest{
		GenerationGetRequest: req,
	})
	if errCall != nil {
		return aigc.ContentGeneration{}, errCall
	}
	return resp, nil
}

func (s rpcContentGenerationStore) Patch(ctx context.Context, req aigc.GenerationPatchRequest) (aigc.ContentGeneration, error) {
	resp, errCall := callPlugin[aigc.ContentGeneration](ctx, s.client, pluginabi.MethodContentGenerationStorePatch, rpcContentGenerationStorePatchRequest{
		GenerationPatchRequest: req,
	})
	if errCall != nil {
		return aigc.ContentGeneration{}, errCall
	}
	return resp, nil
}

func (s rpcContentGenerationStore) Claim(ctx context.Context, req aigc.GenerationClaimRequest) ([]aigc.ContentGeneration, error) {
	resp, errCall := callPlugin[[]aigc.ContentGeneration](ctx, s.client, pluginabi.MethodContentGenerationStoreClaim, rpcContentGenerationStoreClaimRequest{
		GenerationClaimRequest: req,
	})
	if errCall != nil {
		return nil, errCall
	}
	return resp, nil
}

func (s rpcContentGenerationStore) Release(ctx context.Context, req aigc.GenerationReleaseRequest) error {
	_, errCall := callPlugin[rpcEmptyResponse](ctx, s.client, pluginabi.MethodContentGenerationStoreRelease, rpcContentGenerationStoreReleaseRequest{
		GenerationReleaseRequest: req,
	})
	return errCall
}

func (s rpcContentGenerationStore) DeleteExpired(ctx context.Context, req aigc.GenerationDeleteExpiredRequest) (int64, error) {
	resp, errCall := callPlugin[rpcContentGenerationDeleteExpiredResponse](ctx, s.client, pluginabi.MethodContentGenerationStoreDeleteExpired, rpcContentGenerationStoreDeleteExpiredRequest{
		GenerationDeleteExpiredRequest: req,
	})
	if errCall != nil {
		return 0, errCall
	}
	return resp.Deleted, nil
}

type rpcContentGenerationMutator struct {
	*rpcPluginAdapter
}

func (m rpcContentGenerationMutator) MutateContentGeneration(ctx context.Context, req aigc.GenerationMutationRequest) (aigc.GenerationMutationResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	resp, errCall := callPlugin[aigc.GenerationMutationResponse](ctx, m.client, pluginabi.MethodContentGenerationMutate, rpcContentGenerationMutateRequest{
		GenerationMutationRequest: req,
	})
	if errCall != nil {
		return aigc.GenerationMutationResponse{}, errCall
	}
	return resp, nil
}

type rpcContentGenerationDriver struct {
	*rpcPluginAdapter
}

func (d rpcContentGenerationDriver) Supports(ctx context.Context, req aigc.GenerationSupportRequest) (aigc.GenerationSupportResponse, error) {
	resp, errCall := callPlugin[aigc.GenerationSupportResponse](ctx, d.client, pluginabi.MethodContentGenerationDriverSupports, rpcContentGenerationDriverSupportsRequest{
		GenerationSupportRequest: req,
	})
	if errCall != nil {
		return aigc.GenerationSupportResponse{}, errCall
	}
	return resp, nil
}

func (d rpcContentGenerationDriver) PrepareSubmit(ctx context.Context, input aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
	resp, errCall := callPlugin[aigc.GenerationExecutionRequest](ctx, d.client, pluginabi.MethodContentGenerationDriverPrepareSubmit, rpcContentGenerationDriverPrepareSubmitRequest{
		GenerationSubmitInput: input,
	})
	if errCall != nil {
		return aigc.GenerationExecutionRequest{}, errCall
	}
	return resp, nil
}

func (d rpcContentGenerationDriver) ParseSubmit(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error) {
	result, errCall := callPlugin[aigc.GenerationSubmitResult](ctx, d.client, pluginabi.MethodContentGenerationDriverParseSubmit, rpcContentGenerationDriverParseSubmitRequest{
		Response: resp,
	})
	if errCall != nil {
		return aigc.GenerationSubmitResult{}, errCall
	}
	return result, nil
}

func (d rpcContentGenerationDriver) PreparePoll(ctx context.Context, input aigc.GenerationPollInput) (aigc.GenerationExecutionRequest, error) {
	resp, errCall := callPlugin[aigc.GenerationExecutionRequest](ctx, d.client, pluginabi.MethodContentGenerationDriverPreparePoll, rpcContentGenerationDriverPreparePollRequest{
		GenerationPollInput: input,
	})
	if errCall != nil {
		return aigc.GenerationExecutionRequest{}, errCall
	}
	return resp, nil
}

func (d rpcContentGenerationDriver) ParsePoll(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	result, errCall := callPlugin[aigc.GenerationPollResult](ctx, d.client, pluginabi.MethodContentGenerationDriverParsePoll, rpcContentGenerationDriverParsePollRequest{
		Response: resp,
	})
	if errCall != nil {
		return aigc.GenerationPollResult{}, errCall
	}
	return result, nil
}

func (d rpcContentGenerationDriver) PrepareCancel(ctx context.Context, input aigc.GenerationCancelInput) (aigc.GenerationExecutionRequest, error) {
	resp, errCall := callPlugin[aigc.GenerationExecutionRequest](ctx, d.client, pluginabi.MethodContentGenerationDriverPrepareCancel, rpcContentGenerationDriverPrepareCancelRequest{
		GenerationCancelInput: input,
	})
	if errCall != nil {
		return aigc.GenerationExecutionRequest{}, errCall
	}
	return resp, nil
}

func (d rpcContentGenerationDriver) ParseCancel(ctx context.Context, resp aigc.GenerationExecutionResponse) error {
	_, errCall := callPlugin[rpcEmptyResponse](ctx, d.client, pluginabi.MethodContentGenerationDriverParseCancel, rpcContentGenerationDriverParseCancelRequest{
		Response: resp,
	})
	return errCall
}

type rpcContentGenerationObserver struct {
	*rpcPluginAdapter
}

func (o rpcContentGenerationObserver) OnContentGenerationEvent(ctx context.Context, event aigc.ContentGenerationEvent) error {
	if ctx == nil {
		ctx = context.Background()
	}
	_, errCall := callPlugin[rpcEmptyResponse](ctx, o.client, pluginabi.MethodContentGenerationEvent, rpcContentGenerationEventRequest{
		Event: event,
	})
	return errCall
}

// ContentGenerationStore returns the active exclusive ContentGenerationStore plugin.
func (h *Host) ContentGenerationStore() (aigc.ContentGenerationStore, string, bool) {
	if h == nil {
		return nil, "", false
	}
	records := h.activeRecords()
	for _, record := range records {
		if record.plugin.Capabilities.ContentGenerationStore != nil && !h.isPluginFused(record.id) {
			return record.plugin.Capabilities.ContentGenerationStore, record.id, true
		}
	}
	return nil, "", false
}

// MutateContentGeneration runs all active ContentGenerationMutator plugins in priority order.
func (h *Host) MutateContentGeneration(ctx context.Context, req aigc.GenerationMutationRequest) (aigc.GenerationMutationResponse, error) {
	if h == nil {
		return aigc.GenerationMutationResponse{
			Draft:      req.Draft,
			Generation: &req.Generation,
		}, nil
	}
	currentDraft := req.Draft
	currentGen := req.Generation
	currentArtifacts := req.Generation.Artifacts

	records := h.activeRecords()
	for _, record := range records {
		mutator := record.plugin.Capabilities.ContentGenerationMutator
		if mutator == nil || h.isPluginFused(record.id) {
			continue
		}

		stepReq := req
		stepReq.Draft = currentDraft
		stepReq.Generation = currentGen
		if len(currentArtifacts) > 0 {
			stepReq.Generation.Artifacts = currentArtifacts
		}

		resp, errMutate := h.callContentGenerationMutator(ctx, record, mutator, stepReq)
		if errMutate != nil {
			return aigc.GenerationMutationResponse{}, errMutate
		}
		if resp.Reject {
			return resp, nil
		}
		if resp.Draft != nil {
			currentDraft = resp.Draft
		}
		if resp.Generation != nil {
			currentGen = *resp.Generation
		}
		if len(resp.Artifacts) > 0 {
			currentArtifacts = resp.Artifacts
			currentGen.Artifacts = currentArtifacts
		}
	}
	return aigc.GenerationMutationResponse{
		Draft:      currentDraft,
		Generation: &currentGen,
		Artifacts:  currentArtifacts,
	}, nil
}

func (h *Host) callContentGenerationMutator(ctx context.Context, record capabilityRecord, mutator aigc.ContentGenerationMutator, req aigc.GenerationMutationRequest) (resp aigc.GenerationMutationResponse, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			h.fusePlugin(record.id, "ContentGenerationMutator.MutateContentGeneration", recovered)
			resp = aigc.GenerationMutationResponse{}
			err = fmt.Errorf("content generation mutator panic in plugin %s: %v", record.id, recovered)
		}
	}()
	return mutator.MutateContentGeneration(ctx, req)
}

// EmitContentGenerationEvent notifies all active ContentGenerationObserver plugins of a lifecycle event.
func (h *Host) EmitContentGenerationEvent(ctx context.Context, event aigc.ContentGenerationEvent) {
	if h == nil {
		return
	}
	records := h.activeRecords()
	for _, record := range records {
		observer := record.plugin.Capabilities.ContentGenerationObserver
		if observer == nil || h.isPluginFused(record.id) {
			continue
		}
		go func(rec capabilityRecord, obs aigc.ContentGenerationObserver) {
			defer func() {
				if recovered := recover(); recovered != nil {
					h.fusePlugin(rec.id, "ContentGenerationObserver.OnContentGenerationEvent", recovered)
					log.Warnf("pluginhost: content generation observer panic in %s: %v", rec.id, recovered)
				}
			}()
			obsCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := obs.OnContentGenerationEvent(obsCtx, event); err != nil {
				log.Warnf("pluginhost: content generation observer error in %s: %v", rec.id, err)
			}
		}(record, observer)
	}
}

// ContentGenerationDriverFor finds the first active driver plugin supporting the given kind and model.
func (h *Host) ContentGenerationDriverFor(ctx context.Context, kind aigc.ContentKind, model string) (aigc.ContentGenerationDriver, string, bool) {
	if h == nil {
		return nil, "", false
	}
	records := h.activeRecords()
	req := aigc.GenerationSupportRequest{
		Kind:  kind,
		Model: model,
	}
	for _, record := range records {
		driver := record.plugin.Capabilities.ContentGenerationDriver
		if driver == nil || h.isPluginFused(record.id) {
			continue
		}
		supported, ok := h.callContentGenerationDriverSupports(ctx, record, driver, req)
		if ok && supported.Supported {
			return driver, record.id, true
		}
	}
	return nil, "", false
}

func (h *Host) callContentGenerationDriverSupports(ctx context.Context, record capabilityRecord, driver aigc.ContentGenerationDriver, req aigc.GenerationSupportRequest) (resp aigc.GenerationSupportResponse, ok bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			h.fusePlugin(record.id, "ContentGenerationDriver.Supports", recovered)
			resp = aigc.GenerationSupportResponse{}
			ok = false
		}
	}()
	res, err := driver.Supports(ctx, req)
	if err != nil {
		log.Debugf("pluginhost: driver %s supports check failed: %v", record.id, err)
		return aigc.GenerationSupportResponse{}, false
	}
	return res, true
}
