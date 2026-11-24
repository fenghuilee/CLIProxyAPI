package main

import (
	"context"
	"log"
	"strings"
	"sync"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
	"golang.org/x/sync/errgroup"
)

// Mutator implements aigc.ContentGenerationMutator to archive remote media to permanent TOS.
type Mutator struct {
	cfg        Config
	transferer MediaTransferer
}

// NewMutator initializes mutator with config and transfer engine.
func NewMutator(cfg Config, transferer MediaTransferer) *Mutator {
	return &Mutator{
		cfg:        cfg,
		transferer: transferer,
	}
}

// MutateContentGeneration intercepts PhaseBeforeComplete to archive remote media assets.
func (m *Mutator) MutateContentGeneration(ctx context.Context, req aigc.GenerationMutationRequest) (aigc.GenerationMutationResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if req.Phase != aigc.PhaseBeforeComplete || m.transferer == nil {
		return aigc.GenerationMutationResponse{
			Draft:      req.Draft,
			Generation: &req.Generation,
			Artifacts:  req.Generation.Artifacts,
		}, nil
	}

	return m.handleBeforeComplete(ctx, req)
}

func (m *Mutator) handleBeforeComplete(ctx context.Context, req aigc.GenerationMutationRequest) (aigc.GenerationMutationResponse, error) {
	updatedGen := req.Generation
	artifacts := req.Generation.Artifacts
	if len(artifacts) == 0 && req.Metadata != nil {
		if rawArts, ok := req.Metadata["artifacts"].([]aigc.ContentGenerationArtifact); ok {
			artifacts = rawArts
		}
	}

	if len(artifacts) == 0 {
		return aigc.GenerationMutationResponse{
			Generation: &updatedGen,
			Artifacts:  artifacts,
		}, nil
	}

	// Filter by ownership: if SystemOnly is true, only archive generations owned by system (user_id == 0)
	if m.cfg.SystemOnly && req.Generation.UserID != 0 {
		return aigc.GenerationMutationResponse{
			Generation: &updatedGen,
			Artifacts:  artifacts,
		}, nil
	}

	var g errgroup.Group
	g.SetLimit(m.cfg.Concurrency)

	var mu sync.Mutex
	newArtifacts := make([]aigc.ContentGenerationArtifact, len(artifacts))
	urlReplacements := make(map[string]string)
	archivedKeys := make([]string, 0)

	for i, art := range artifacts {
		idx := i
		item := art
		g.Go(func() error {
			oldURI := strings.TrimSpace(item.URI)
			if m.transferer.ShouldTransfer(oldURI) {
				res, err := m.transferer.Transfer(ctx, oldURI, item.MIMEType)
				if err != nil {
					// Graceful fallback: never fail the generation if archiving fails.
					// Keep the provider's original URL so that upstream billing and delivery are preserved.
					log.Printf("[aigc-media-persist] warning: failed to archive media %s (task: %s): %v. Preserving original upstream URL.", oldURI, updatedGen.ID, err)
					newArtifacts[idx] = item
					return nil
				}

				// Archival succeeded: upgrade artifact to permanent TOS storage
				item.StorageProvider = "tos"
				item.URI = res.NewURL
				if res.MIMEType != "" {
					item.MIMEType = res.MIMEType
				}
				if res.SizeBytes > 0 {
					item.SizeBytes = res.SizeBytes
				}
				if res.SHA256 != "" {
					item.SHA256 = res.SHA256
				}

				mu.Lock()
				urlReplacements[oldURI] = res.NewURL
				if res.ObjectKey != "" {
					archivedKeys = append(archivedKeys, res.ObjectKey)
				}
				mu.Unlock()
			}
			newArtifacts[idx] = item
			return nil
		})
	}

	_ = g.Wait()
	updatedGen.Artifacts = newArtifacts

	// Synchronize Output JSON if any URL was replaced
	if len(urlReplacements) > 0 && len(updatedGen.Output) > 0 {
		outputStr := string(updatedGen.Output)
		for oldURL, newURL := range urlReplacements {
			outputStr = strings.ReplaceAll(outputStr, oldURL, newURL)
		}
		updatedGen.Output = []byte(outputStr)
	}

	// Record telemetry metadata if objects were archived
	if len(archivedKeys) > 0 {
		if updatedGen.Metadata == nil {
			updatedGen.Metadata = make(map[string]any)
		}
		updatedGen.Metadata["aigc-media-persist"] = map[string]any{
			"archived_objects": archivedKeys,
		}
	}

	return aigc.GenerationMutationResponse{
		Generation: &updatedGen,
		Artifacts:  newArtifacts,
	}, nil
}
