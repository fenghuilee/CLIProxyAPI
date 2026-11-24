package aigc

// IsTerminalStatus reports whether the status is terminal (succeeded, failed, canceled).
func IsTerminalStatus(status GenerationStatus) bool {
	switch status {
	case StatusSucceeded, StatusFailed, StatusCanceled:
		return true
	default:
		return false
	}
}

// ValidateStatusTransition verifies if changing from oldStatus to newStatus is allowed.
func ValidateStatusTransition(oldStatus, newStatus GenerationStatus) error {
	if oldStatus == newStatus {
		return nil
	}
	if IsTerminalStatus(oldStatus) {
		return ErrInvalidStateTransition
	}
	switch oldStatus {
	case StatusAccepted:
		if newStatus == StatusRunning || newStatus == StatusFailed || newStatus == StatusCanceled || newStatus == StatusSucceeded {
			return nil
		}
	case StatusRunning:
		if newStatus == StatusSucceeded || newStatus == StatusFailed || newStatus == StatusCanceled {
			return nil
		}
	}
	return ErrInvalidStateTransition
}

// ValidateStageTransition verifies if transitioning stage is consistent with current status.
func ValidateStageTransition(status GenerationStatus, stage GenerationStage) error {
	switch status {
	case StatusAccepted:
		switch stage {
		case StageCreated, StagePreparingInput, StageInputReady, StageSelectingProvider, StageSubmitting:
			return nil
		}
	case StatusRunning:
		switch stage {
		case StageProviderRunning, StagePolling, StagePreparingOutput:
			return nil
		}
	case StatusSucceeded:
		if stage == StageCompleted {
			return nil
		}
	case StatusFailed:
		if stage == StageFailed {
			return nil
		}
	case StatusCanceled:
		if stage == StageCanceled {
			return nil
		}
	}
	return ErrInvalidStateTransition
}
