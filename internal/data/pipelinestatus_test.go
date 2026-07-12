package data

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func allPipelineStatusHelpers() map[string]func(string) bool {
	return map[string]func(string) bool{
		"IsPending": IsPending,
		"IsSuccess": IsSuccess,
		"IsFailure": IsFailure,
		"IsSkipped": IsSkipped,
		"IsNeutral": IsNeutral,
		"IsManual":  IsManual,
	}
}

func TestPipelineStatusConstants_MatchExpectedWireValues(t *testing.T) {
	tests := []struct {
		name   string
		status PipelineStatus
		want   string
	}{
		{"StatusCreated", StatusCreated, "created"},
		{"StatusWaitingForResource", StatusWaitingForResource, "waiting_for_resource"},
		{"StatusPreparing", StatusPreparing, "preparing"},
		{"StatusPending", StatusPending, "pending"},
		{"StatusRunning", StatusRunning, "running"},
		{"StatusSuccess", StatusSuccess, "success"},
		{"StatusFailed", StatusFailed, "failed"},
		{"StatusCanceled", StatusCanceled, "canceled"},
		{"StatusSkipped", StatusSkipped, "skipped"},
		{"StatusManual", StatusManual, "manual"},
		{"StatusScheduled", StatusScheduled, "scheduled"},
		{"StatusWaitingForCallback", StatusWaitingForCallback, "waiting_for_callback"},
		{"StatusCanceling", StatusCanceling, "canceling"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, PipelineStatus(tt.want), tt.status)
		})
	}
}

func TestPipelineStatusHelpers_MutualExclusivity(t *testing.T) {
	tests := []struct {
		status     PipelineStatus
		wantHelper string
	}{
		{StatusCreated, "IsPending"},
		{StatusWaitingForResource, "IsPending"},
		{StatusPreparing, "IsPending"},
		{StatusPending, "IsPending"},
		{StatusRunning, "IsPending"},
		{StatusScheduled, "IsPending"},
		{StatusWaitingForCallback, "IsPending"},
		{StatusCanceling, "IsPending"},
		{StatusSuccess, "IsSuccess"},
		{StatusFailed, "IsFailure"},
		{StatusSkipped, "IsSkipped"},
		{StatusCanceled, "IsNeutral"},
		{StatusManual, "IsManual"},
	}

	helpers := allPipelineStatusHelpers()

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			var matched []string
			for name, isFn := range helpers {
				if isFn(string(tt.status)) {
					matched = append(matched, name)
				}
			}

			assert.Len(
				t,
				matched,
				1,
				"status %q must match exactly one of the six helpers, matched %v",
				tt.status,
				matched,
			)
			assert.Contains(
				t,
				matched,
				tt.wantHelper,
				"status %q was expected to match %s, matched %v instead",
				tt.status,
				tt.wantHelper,
				matched,
			)
		})
	}
}

func TestIsPending(t *testing.T) {
	t.Run("returns true for every non-terminal queued or in-progress status", func(t *testing.T) {
		pendingStatuses := []PipelineStatus{
			StatusCreated,
			StatusWaitingForResource,
			StatusPreparing,
			StatusPending,
			StatusRunning,
			StatusScheduled,
			StatusWaitingForCallback,
			StatusCanceling,
		}

		for _, status := range pendingStatuses {
			assert.True(t, IsPending(string(status)), "expected IsPending(%q) to be true", status)
		}
	})

	t.Run("returns false for every terminal or manual status", func(t *testing.T) {
		nonPendingStatuses := []PipelineStatus{
			StatusSuccess,
			StatusFailed,
			StatusSkipped,
			StatusCanceled,
			StatusManual,
		}

		for _, status := range nonPendingStatuses {
			assert.False(t, IsPending(string(status)), "expected IsPending(%q) to be false", status)
		}
	})
}

func TestPipelineStatusHelpers_UnknownAndEmptyStatusMatchNoHelper(t *testing.T) {
	helpers := allPipelineStatusHelpers()

	tests := []struct {
		name   string
		status string
	}{
		{"empty string", ""},
		{"unrecognized status string", "unknown_status_xyz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for helperName, isFn := range helpers {
				assert.False(
					t,
					isFn(tt.status),
					"expected %s(%q) to be false",
					helperName,
					tt.status,
				)
			}
		})
	}
}

func TestPipelineStatusHelpers_AreCaseSensitiveAndDoNotMatchUppercaseGraphQLStyleValues(
	t *testing.T,
) {
	t.Run(
		"IsSuccess does not recognize the uppercase GraphQL-style SUCCESS value",
		func(t *testing.T) {
			assert.False(
				t,
				IsSuccess("SUCCESS"),
				"helpers in this package are intentionally case-sensitive and only recognize the "+
					"lowercase REST-style values (e.g. %q); GraphQL's PipelineStatusEnum/CiJobStatus "+
					"returns uppercase values (e.g. SUCCESS), and normalizing that casing to lowercase "+
					"before it reaches this package is the responsibility of the conversion layer in "+
					"prapi.go that populates PipelineStatus, not of this adapter package",
				string(StatusSuccess),
			)
		},
	)

	t.Run("IsSuccess still recognizes the lowercase REST-style success value", func(t *testing.T) {
		assert.True(t, IsSuccess(string(StatusSuccess)))
	})

	t.Run(
		"uppercase variants of the remaining helpers are equally unrecognized",
		func(t *testing.T) {
			assert.False(t, IsPending("RUNNING"), "expected IsPending(%q) to be false", "RUNNING")
			assert.False(t, IsFailure("FAILED"), "expected IsFailure(%q) to be false", "FAILED")
			assert.False(t, IsSkipped("SKIPPED"), "expected IsSkipped(%q) to be false", "SKIPPED")
			assert.False(t, IsNeutral("CANCELED"), "expected IsNeutral(%q) to be false", "CANCELED")
			assert.False(t, IsManual("MANUAL"), "expected IsManual(%q) to be false", "MANUAL")
		},
	)
}
