package prview

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prssection"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/tasks"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
)

func (m *Model) label(labels []string) tea.Cmd {
	pr := m.pr.Data.Primary
	prNumber := pr.GetNumber()
	taskId := fmt.Sprintf("pr_label_%d", prNumber)
	task := context.Task{
		Id:           taskId,
		StartText:    fmt.Sprintf("Labeling mr #%d to %s", prNumber, labels),
		FinishedText: fmt.Sprintf("mr #%d has been labeled with %s", prNumber, labels),
		State:        context.TaskStart,
		Error:        nil,
	}

	labelsMap := make(map[string]bool)
	for _, label := range labels {
		labelsMap[label] = true
	}

	existingLabelsColorMap := make(map[string]string)
	for _, label := range m.pr.Data.Primary.Labels.Nodes {
		existingLabelsColorMap[label.Name] = label.Color
	}

	var toRemove []string
	for _, label := range m.pr.Data.Primary.Labels.Nodes {
		if _, ok := labelsMap[label.Name]; !ok {
			toRemove = append(toRemove, label.Name)
		}
	}

	startCmd := m.ctx.StartTask(task)
	return tea.Batch(startCmd, func() tea.Msg {
		err := data.UpdateMergeRequestLabels(pr.GetRepoNameWithOwner(), prNumber, labels, toRemove)

		returnedLabels := data.PRLabels{Nodes: []data.Label{}}
		for _, label := range labels {
			returnedLabels.Nodes = append(returnedLabels.Nodes, data.Label{
				Name:  label,
				Color: existingLabelsColorMap[label],
			})
		}
		return constants.TaskFinishedMsg{
			SectionId:   m.sectionId,
			SectionType: prssection.SectionType,
			TaskId:      taskId,
			Err:         err,
			Msg: tasks.UpdatePRMsg{
				PrNumber: prNumber,
				Labels:   &returnedLabels,
			},
		}
	})
}
