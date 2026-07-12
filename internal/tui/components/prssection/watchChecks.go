package prssection

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/log/v2"
	"github.com/gen2brain/beeep"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/tasks"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
)

const watchChecksPollInterval = 5 * time.Second

type watchOutcome int

const (
	watchOutcomeReschedule watchOutcome = iota
	watchOutcomeSuccess
	watchOutcomeFailure
	watchOutcomeNeutral
	watchOutcomeError
)

func decideWatchOutcome(pipeline data.MergeRequestPipeline, err error) watchOutcome {
	if err != nil {
		return watchOutcomeError
	}
	status := string(pipeline.Status)
	if pipeline.ID == 0 || data.IsPending(status) || data.IsManual(status) {
		return watchOutcomeReschedule
	}
	if data.IsFailure(status) {
		return watchOutcomeFailure
	}
	if data.IsSkipped(status) || data.IsNeutral(status) {
		return watchOutcomeNeutral
	}
	return watchOutcomeSuccess
}

type watchPipelineTickMsg struct {
	taskId            string
	sectionId         int
	repoNameWithOwner string
	prNumber          int
	prTitle           string
}

type watchPipelineResultMsg struct {
	taskId            string
	sectionId         int
	repoNameWithOwner string
	prNumber          int
	prTitle           string
	pipeline          data.MergeRequestPipeline
	err               error
}

func (m *Model) watchChecks() tea.Cmd {
	pr := m.GetCurrRow()
	if pr == nil {
		return nil
	}

	prNumber := pr.GetNumber()
	taskId := fmt.Sprintf("pr_watch_checks_%d", prNumber)
	task := context.Task{
		Id:           taskId,
		StartText:    fmt.Sprintf("Watching checks for PR #%d", prNumber),
		FinishedText: fmt.Sprintf("Watching checks for PR #%d", prNumber),
		State:        context.TaskStart,
		Error:        nil,
	}
	startCmd := m.Ctx.StartTask(task)

	return tea.Batch(
		startCmd,
		fetchPipelineStatusCmd(taskId, m.Id, pr.GetRepoNameWithOwner(), prNumber, pr.GetTitle()),
	)
}

func fetchPipelineStatusCmd(
	taskId string,
	sectionId int,
	repoNameWithOwner string,
	prNumber int,
	prTitle string,
) tea.Cmd {
	return func() tea.Msg {
		pipeline, err := data.FindPipelineForMR(repoNameWithOwner, prNumber)
		return watchPipelineResultMsg{
			taskId:            taskId,
			sectionId:         sectionId,
			repoNameWithOwner: repoNameWithOwner,
			prNumber:          prNumber,
			prTitle:           prTitle,
			pipeline:          pipeline,
			err:               err,
		}
	}
}

func watchPipelineTickCmd(
	taskId string,
	sectionId int,
	repoNameWithOwner string,
	prNumber int,
	prTitle string,
) tea.Cmd {
	return tea.Tick(watchChecksPollInterval, func(time.Time) tea.Msg {
		return watchPipelineTickMsg{
			taskId:            taskId,
			sectionId:         sectionId,
			repoNameWithOwner: repoNameWithOwner,
			prNumber:          prNumber,
			prTitle:           prTitle,
		}
	})
}

func (m *Model) onWatchPipelineTickMsg(msg watchPipelineTickMsg) tea.Cmd {
	if msg.sectionId != m.Id {
		return nil
	}
	return fetchPipelineStatusCmd(
		msg.taskId,
		msg.sectionId,
		msg.repoNameWithOwner,
		msg.prNumber,
		msg.prTitle,
	)
}

func (m *Model) onWatchPipelineResultMsg(msg watchPipelineResultMsg) tea.Cmd {
	if msg.sectionId != m.Id {
		return nil
	}

	switch decideWatchOutcome(msg.pipeline, msg.err) {
	case watchOutcomeReschedule:
		return watchPipelineTickCmd(
			msg.taskId,
			msg.sectionId,
			msg.repoNameWithOwner,
			msg.prNumber,
			msg.prTitle,
		)
	case watchOutcomeError:
		return finishWatchChecks(msg.taskId, msg.sectionId, msg.prNumber, msg.err)
	case watchOutcomeFailure:
		notifyWatchChecksResult(msg, "❌ Checks have failed")
		return finishWatchChecks(msg.taskId, msg.sectionId, msg.prNumber, nil)
	case watchOutcomeNeutral:
		notifyWatchChecksResult(
			msg,
			fmt.Sprintf(" Checks finished: %s", string(msg.pipeline.Status)),
		)
		return finishWatchChecks(msg.taskId, msg.sectionId, msg.prNumber, nil)
	default:
		notifyWatchChecksResult(msg, "✅ Checks have passed")
		return finishWatchChecks(msg.taskId, msg.sectionId, msg.prNumber, nil)
	}
}

func notifyWatchChecksResult(msg watchPipelineResultMsg, summary string) {
	err := beeep.Notify(
		fmt.Sprintf("gh-dash: %s", msg.prTitle),
		fmt.Sprintf("PR #%d in %s\n%s", msg.prNumber, msg.repoNameWithOwner, summary),
		"",
	)
	if err != nil {
		log.Error("Error showing system notification", "err", err)
	}
}

func finishWatchChecks(taskId string, sectionId, prNumber int, err error) tea.Cmd {
	return func() tea.Msg {
		return constants.TaskFinishedMsg{
			TaskId:      taskId,
			SectionId:   sectionId,
			SectionType: SectionType,
			Err:         err,
			Msg: tasks.UpdatePRMsg{
				PrNumber: prNumber,
			},
		}
	}
}
