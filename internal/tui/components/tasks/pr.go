package tasks

import (
	"errors"
	"fmt"
	"io"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/log/v2"
	"github.com/cli/browser"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/git"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/utils"
)

func init() {
	browser.Stdout = io.Discard
	browser.Stderr = io.Discard
}

var openURL = browser.OpenURL

type SectionIdentifier struct {
	Id   int
	Type string
}

type UpdatePRMsg struct {
	PrNumber         int
	IsClosed         *bool
	NewComment       *data.Comment
	ReadyForReview   *bool
	IsMerged         *bool
	AddedAssignees   *data.Assignees
	RemovedAssignees *data.Assignees
	Labels           *data.PRLabels
}

type UpdateBranchMsg struct {
	Name      string
	IsCreated *bool
	NewPr     *data.PullRequestData
}

func buildTaskId(prefix string, prNumber int) string {
	return fmt.Sprintf("%s_%d", prefix, prNumber)
}

func runMRAction(
	ctx *context.ProgramContext,
	section SectionIdentifier,
	taskId, startText, finishedText string,
	do func() (tea.Msg, error),
) tea.Cmd {
	task := context.Task{
		Id:           taskId,
		StartText:    startText,
		FinishedText: finishedText,
		State:        context.TaskStart,
		Error:        nil,
	}
	startCmd := ctx.StartTask(task)
	return tea.Batch(startCmd, func() tea.Msg {
		msg, err := do()
		return constants.TaskFinishedMsg{
			TaskId:      taskId,
			SectionId:   section.Id,
			SectionType: section.Type,
			Err:         err,
			Msg:         msg,
		}
	})
}

func OpenBranchPR(ctx *context.ProgramContext, section SectionIdentifier, branch string) tea.Cmd {
	return runMRAction(
		ctx, section,
		fmt.Sprintf("branch_open_%s", branch),
		fmt.Sprintf("Opening MR for branch %s", branch),
		fmt.Sprintf("MR for branch %s has been opened", branch),
		func() (tea.Msg, error) {
			projectPath := git.GetRepoShortName(ctx.RepoUrl)
			webURL, err := data.FindMergeRequestWebURLByBranch(projectPath, branch)
			if err != nil {
				return UpdatePRMsg{}, err
			}
			return UpdatePRMsg{}, openURL(webURL)
		},
	)
}

func ReopenPR(ctx *context.ProgramContext, section SectionIdentifier, pr data.RowData) tea.Cmd {
	prNumber := pr.GetNumber()
	return runMRAction(
		ctx, section,
		buildTaskId("pr_reopen", prNumber),
		fmt.Sprintf("Reopening MR #%d", prNumber),
		fmt.Sprintf("MR #%d has been reopened", prNumber),
		func() (tea.Msg, error) {
			err := data.ReopenMergeRequest(pr.GetRepoNameWithOwner(), prNumber)
			return UpdatePRMsg{PrNumber: prNumber, IsClosed: utils.BoolPtr(false)}, err
		},
	)
}

func ClosePR(ctx *context.ProgramContext, section SectionIdentifier, pr data.RowData) tea.Cmd {
	prNumber := pr.GetNumber()
	return runMRAction(
		ctx, section,
		buildTaskId("pr_close", prNumber),
		fmt.Sprintf("Closing MR #%d", prNumber),
		fmt.Sprintf("MR #%d has been closed", prNumber),
		func() (tea.Msg, error) {
			err := data.CloseMergeRequest(pr.GetRepoNameWithOwner(), prNumber)
			return UpdatePRMsg{PrNumber: prNumber, IsClosed: utils.BoolPtr(true)}, err
		},
	)
}

func PRReady(ctx *context.ProgramContext, section SectionIdentifier, pr data.RowData) tea.Cmd {
	prNumber := pr.GetNumber()
	return runMRAction(
		ctx, section,
		buildTaskId("pr_ready", prNumber),
		fmt.Sprintf("Marking MR #%d as ready for review", prNumber),
		fmt.Sprintf("MR #%d has been marked as ready for review", prNumber),
		func() (tea.Msg, error) {
			err := data.MarkMergeRequestReady(pr.GetRepoNameWithOwner(), prNumber)
			return UpdatePRMsg{PrNumber: prNumber, ReadyForReview: utils.BoolPtr(true)}, err
		},
	)
}

func MergePR(ctx *context.ProgramContext, section SectionIdentifier, pr data.RowData) tea.Cmd {
	prNumber := pr.GetNumber()
	return runMRAction(
		ctx, section,
		fmt.Sprintf("merge_%d", prNumber),
		fmt.Sprintf("Merging MR #%d", prNumber),
		fmt.Sprintf("MR #%d has been merged", prNumber),
		func() (tea.Msg, error) {
			err := data.AcceptMergeRequest(pr.GetRepoNameWithOwner(), prNumber)
			isMerged := err == nil
			return UpdatePRMsg{PrNumber: prNumber, IsMerged: &isMerged}, err
		},
	)
}

func CreatePR(
	ctx *context.ProgramContext,
	section SectionIdentifier,
	branchName string,
	title string,
) tea.Cmd {
	taskId := fmt.Sprintf("create_pr_%s", title)
	task := context.Task{
		Id:           taskId,
		StartText:    fmt.Sprintf(`Creating MR "%s"`, title),
		FinishedText: fmt.Sprintf(`MR "%s" has been created`, title),
		State:        context.TaskStart,
		Error:        nil,
	}
	startCmd := ctx.StartTask(task)

	return tea.Batch(startCmd, func() tea.Msg {
		projectPath := git.GetRepoShortName(ctx.RepoUrl)
		var isCreated bool
		targetBranch, err := data.ProjectDefaultBranch(projectPath)
		if err != nil {
			log.Error("failed to resolve default branch", "project", projectPath, "err", err)
		} else if createErr := data.CreateMergeRequest(projectPath, branchName, targetBranch, title); createErr != nil {
			log.Error("failed to create merge request", "branch", branchName, "err", createErr)
		} else {
			isCreated = true
		}
		return constants.TaskFinishedMsg{
			SectionId:   section.Id,
			SectionType: section.Type,
			TaskId:      taskId,
			Err:         nil,
			Msg:         UpdateBranchMsg{Name: branchName, IsCreated: &isCreated},
		}
	})
}

func UpdatePR(ctx *context.ProgramContext, section SectionIdentifier, pr data.RowData) tea.Cmd {
	prNumber := pr.GetNumber()
	return runMRAction(
		ctx, section,
		buildTaskId("pr_update", prNumber),
		fmt.Sprintf("Updating MR #%d", prNumber),
		fmt.Sprintf("MR #%d has been updated", prNumber),
		func() (tea.Msg, error) {
			err := data.RebaseMergeRequest(pr.GetRepoNameWithOwner(), prNumber)
			return UpdatePRMsg{PrNumber: prNumber}, err
		},
	)
}

func toAssignees(usernames []string) data.Assignees {
	nodes := make([]data.Assignee, 0, len(usernames))
	for _, assignee := range usernames {
		nodes = append(nodes, data.Assignee{Login: assignee})
	}
	return data.Assignees{Nodes: nodes}
}

func AssignPR(
	ctx *context.ProgramContext,
	section SectionIdentifier,
	pr data.RowData,
	usernames []string,
) tea.Cmd {
	prNumber := pr.GetNumber()
	return runMRAction(
		ctx, section,
		buildTaskId("pr_assign", prNumber),
		fmt.Sprintf("Assigning mr #%d to %s", prNumber, usernames),
		fmt.Sprintf("mr #%d has been assigned to %s", prNumber, usernames),
		func() (tea.Msg, error) {
			err := data.AddMergeRequestAssignees(pr.GetRepoNameWithOwner(), prNumber, usernames)
			returnedAssignees := toAssignees(usernames)
			return UpdatePRMsg{PrNumber: prNumber, AddedAssignees: &returnedAssignees}, err
		},
	)
}

func RequestReviewPR(
	ctx *context.ProgramContext,
	section SectionIdentifier,
	pr data.RowData,
	usernames []string,
) tea.Cmd {
	prNumber := pr.GetNumber()
	return runMRAction(
		ctx, section,
		buildTaskId("pr_request_review", prNumber),
		fmt.Sprintf("Requesting review on mr #%d from %s", prNumber, usernames),
		fmt.Sprintf("Review requested on mr #%d from %s", prNumber, usernames),
		func() (tea.Msg, error) {
			err := data.AddMergeRequestReviewers(pr.GetRepoNameWithOwner(), prNumber, usernames)
			return UpdatePRMsg{PrNumber: prNumber}, err
		},
	)
}

func UnassignPR(
	ctx *context.ProgramContext,
	section SectionIdentifier,
	pr data.RowData,
	usernames []string,
) tea.Cmd {
	prNumber := pr.GetNumber()
	return runMRAction(
		ctx, section,
		buildTaskId("pr_unassign", prNumber),
		fmt.Sprintf("Unassigning %s from mr #%d", usernames, prNumber),
		fmt.Sprintf("%s unassigned from mr #%d", usernames, prNumber),
		func() (tea.Msg, error) {
			err := data.RemoveMergeRequestAssignees(pr.GetRepoNameWithOwner(), prNumber, usernames)
			returnedAssignees := toAssignees(usernames)
			return UpdatePRMsg{PrNumber: prNumber, RemovedAssignees: &returnedAssignees}, err
		},
	)
}

func CommentOnPR(
	ctx *context.ProgramContext,
	section SectionIdentifier,
	pr data.RowData,
	body string,
) tea.Cmd {
	prNumber := pr.GetNumber()
	return runMRAction(
		ctx, section,
		buildTaskId("pr_comment", prNumber),
		fmt.Sprintf("Commenting on MR #%d", prNumber),
		fmt.Sprintf("Commented on MR #%d", prNumber),
		func() (tea.Msg, error) {
			err := data.CommentOnMergeRequest(pr.GetRepoNameWithOwner(), prNumber, body)
			return UpdatePRMsg{
				PrNumber: prNumber,
				NewComment: &data.Comment{
					Author:    struct{ Login string }{Login: ctx.User},
					Body:      body,
					UpdatedAt: time.Now(),
				},
			}, err
		},
	)
}

func ApprovePR(
	ctx *context.ProgramContext,
	section SectionIdentifier,
	pr data.RowData,
	comment string,
) tea.Cmd {
	prNumber := pr.GetNumber()
	return runMRAction(
		ctx, section,
		buildTaskId("pr_approve", prNumber),
		fmt.Sprintf("Approving mr #%d", prNumber),
		fmt.Sprintf("mr #%d has been approved", prNumber),
		func() (tea.Msg, error) {
			err := data.ApproveMergeRequest(pr.GetRepoNameWithOwner(), prNumber, comment)
			return UpdatePRMsg{PrNumber: prNumber}, err
		},
	)
}

func ApproveWorkflows(
	ctx *context.ProgramContext,
	section SectionIdentifier,
	pr data.RowData,
) tea.Cmd {
	prNumber := pr.GetNumber()
	repo := pr.GetRepoNameWithOwner()
	taskId := buildTaskId("pr_approve_workflows", prNumber)

	task := context.Task{
		Id:           taskId,
		StartText:    fmt.Sprintf("Approving workflows for MR #%d", prNumber),
		FinishedText: fmt.Sprintf("Workflows for MR #%d have been approved", prNumber),
		State:        context.TaskStart,
		Error:        nil,
	}
	startCmd := ctx.StartTask(task)

	return tea.Batch(startCmd, func() tea.Msg {
		pipeline, err := data.FindPipelineForMR(repo, prNumber)
		if err != nil {
			return constants.TaskFinishedMsg{
				TaskId:      taskId,
				SectionId:   section.Id,
				SectionType: section.Type,
				Err:         fmt.Errorf("failed to locate pipeline: %w", err),
				Msg:         UpdatePRMsg{PrNumber: prNumber},
			}
		}
		if pipeline.ID == 0 {
			return constants.TaskFinishedMsg{
				TaskId:      taskId,
				SectionId:   section.Id,
				SectionType: section.Type,
				Err:         fmt.Errorf("no workflows awaiting approval"),
				Msg:         UpdatePRMsg{PrNumber: prNumber},
			}
		}

		manualJobs, err := data.ListPipelineJobs(repo, pipeline.ID, "manual")
		if err != nil {
			return constants.TaskFinishedMsg{
				TaskId:      taskId,
				SectionId:   section.Id,
				SectionType: section.Type,
				Err:         fmt.Errorf("failed to list manual jobs: %w", err),
				Msg:         UpdatePRMsg{PrNumber: prNumber},
			}
		}
		if len(manualJobs) == 0 {
			return constants.TaskFinishedMsg{
				TaskId:      taskId,
				SectionId:   section.Id,
				SectionType: section.Type,
				Err:         fmt.Errorf("no workflows awaiting approval"),
				Msg:         UpdatePRMsg{PrNumber: prNumber},
			}
		}

		// Play each manual job (best-effort). Manual jobs are not limited to
		// low-risk re-runs: a pipeline commonly gates real deploys behind a
		// manual job (e.g. "deploy-staging"/"deploy-prod"), so every job
		// played here is logged individually for auditability.
		var playErrs []error
		approved := 0
		for _, job := range manualJobs {
			log.Info(
				"Playing manual job",
				"jobId", job.ID,
				"jobName", job.Name,
				"stage", job.Stage,
				"pr", prNumber,
			)
			if err := data.PlayJob(repo, job.ID); err != nil {
				playErrs = append(playErrs, fmt.Errorf("failed to play job %d: %w", job.ID, err))
				continue
			}
			approved++
		}
		log.Info(
			"Finished playing manual jobs",
			"pr", prNumber,
			"played", approved,
			"total", len(manualJobs),
		)

		return constants.TaskFinishedMsg{
			TaskId:      taskId,
			SectionId:   section.Id,
			SectionType: section.Type,
			Err:         errors.Join(playErrs...),
			Msg:         UpdatePRMsg{PrNumber: prNumber},
		}
	})
}
