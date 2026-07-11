package tui

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/cli/browser"

	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
)

func init() {
	browser.Stdout = io.Discard
	browser.Stderr = io.Discard
}

var openURL = browser.OpenURL

func (m *Model) openBrowser() tea.Cmd {
	taskId := fmt.Sprintf("open_browser_%d", time.Now().Unix())
	task := context.Task{
		Id:           taskId,
		StartText:    "Opening in browser",
		FinishedText: "Opened in browser",
		State:        context.TaskStart,
		Error:        nil,
	}
	startCmd := m.ctx.StartTask(task)
	openCmd := func() tea.Msg {
		currRow := m.getCurrRowData()
		if currRow == nil || reflect.ValueOf(currRow).IsNil() {
			return constants.TaskFinishedMsg{
				TaskId: taskId,
				Err:    errors.New("current selection doesn't have a URL"),
			}
		}
		err := openURL(currRow.GetUrl())
		return constants.TaskFinishedMsg{TaskId: taskId, Err: err}
	}
	return tea.Batch(startCmd, openCmd)
}
