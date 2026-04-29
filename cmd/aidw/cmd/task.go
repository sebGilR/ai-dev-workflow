package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"aidw/cmd/aidw/internal/wip"
)

var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "Manage implementation task state machine",
}

var taskNextCmd = &cobra.Command{
	Use:   "next <path>",
	Short: "Get the next uncompleted task from spec.md",
	Args:  cobra.ExactArgs(1),
	Run: func(c *cobra.Command, args []string) {
		task, err := wip.NextTask(args[0])
		if err != nil {
			Die("next-task: %v", err)
		}
		if task == nil {
			PrintJSON(map[string]any{"status": "finished", "message": "All tasks in spec.md are completed."})
			return
		}
		PrintJSON(task)
	},
}

var taskDoneCmd = &cobra.Command{
	Use:   "done <path> <id> <description>",
	Short: "Mark a task as completed in execution.md",
	Args:  cobra.ExactArgs(3),
	Run: func(c *cobra.Command, args []string) {
		repoPath := args[0]
		var id int
		if _, err := fmt.Sscanf(args[1], "%d", &id); err != nil {
			Die("invalid task id: %v", err)
		}
		desc := args[2]

		if err := wip.MarkTaskDone(repoPath, id, desc); err != nil {
			Die("task-done: %v", err)
		}
		fmt.Fprintf(os.Stderr, "Task %d marked as complete.\n", id)
	},
}

func init() {
	taskCmd.AddCommand(taskNextCmd)
	taskCmd.AddCommand(taskDoneCmd)
	Root.AddCommand(taskCmd)
}
