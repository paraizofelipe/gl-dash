package data

type PipelineStatus string

const (
	StatusCreated            PipelineStatus = "created"
	StatusWaitingForResource PipelineStatus = "waiting_for_resource"
	StatusPreparing          PipelineStatus = "preparing"
	StatusPending            PipelineStatus = "pending"
	StatusRunning            PipelineStatus = "running"
	StatusSuccess            PipelineStatus = "success"
	StatusFailed             PipelineStatus = "failed"
	StatusCanceled           PipelineStatus = "canceled"
	StatusSkipped            PipelineStatus = "skipped"
	StatusManual             PipelineStatus = "manual"
	StatusScheduled          PipelineStatus = "scheduled"
	StatusWaitingForCallback PipelineStatus = "waiting_for_callback"
	StatusCanceling          PipelineStatus = "canceling"
)

func IsPending(status string) bool {
	switch PipelineStatus(status) {
	case StatusCreated, StatusWaitingForResource, StatusPreparing,
		StatusPending, StatusRunning, StatusScheduled,
		StatusWaitingForCallback, StatusCanceling:
		return true
	}
	return false
}

func IsSuccess(status string) bool { return PipelineStatus(status) == StatusSuccess }

func IsFailure(status string) bool { return PipelineStatus(status) == StatusFailed }

func IsSkipped(status string) bool { return PipelineStatus(status) == StatusSkipped }

func IsNeutral(status string) bool { return PipelineStatus(status) == StatusCanceled }

func IsManual(status string) bool { return PipelineStatus(status) == StatusManual }
