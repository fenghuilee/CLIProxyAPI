package aigc

// GenerationStatus represents the high-level external lifecycle status.
type GenerationStatus string

const (
	StatusAccepted  GenerationStatus = "accepted"
	StatusRunning   GenerationStatus = "running"
	StatusSucceeded GenerationStatus = "succeeded"
	StatusFailed    GenerationStatus = "failed"
	StatusCanceled  GenerationStatus = "canceled"
)

// GenerationStage represents the granular internal processing stage.
type GenerationStage string

const (
	StageCreated           GenerationStage = "created"
	StagePreparingInput    GenerationStage = "preparing_input"
	StageInputReady        GenerationStage = "input_ready"
	StageSelectingProvider GenerationStage = "selecting_provider"
	StageSubmitting        GenerationStage = "submitting"
	StageProviderRunning   GenerationStage = "provider_running"
	StagePolling           GenerationStage = "polling"
	StagePreparingOutput   GenerationStage = "preparing_output"
	StageCompleted         GenerationStage = "completed"
	StageFailed            GenerationStage = "failed"
	StageCanceled          GenerationStage = "canceled"
)
