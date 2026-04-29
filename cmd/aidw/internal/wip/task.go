package wip

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"aidw/cmd/aidw/internal/util"
)

type Task struct {
	ID          int    `json:"id"`
	Description string `json:"description"`
}

func NextTask(repoPath string) (*Task, error) {
	state, err := EnsureBranchState(repoPath, "")
	if err != nil { return nil, err }

	specPath := filepath.Join(state.WipDir, "spec.md")
	execPath := filepath.Join(state.WipDir, "execution.md")

	tasks, err := parseSpecTasks(specPath)
	if err != nil { return nil, err }

	completed, _ := parseCompletedTasks(execPath)

	for _, t := range tasks {
		if !completed[t.ID] {
			return &t, nil
		}
	}
	return nil, nil
}

func parseSpecTasks(path string) ([]Task, error) {
	file, err := os.Open(path)
	if err != nil { return nil, err }
	defer file.Close()

	var tasks []Task
	scanner := bufio.NewScanner(file)
	id := 1
	inTasks := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		l := strings.ToLower(line)
		if strings.Contains(l, "## implementation") || strings.Contains(l, "## tasks") {
			inTasks = true
			continue
		}
		if inTasks && line != "" && !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "Task") && !strings.HasPrefix(line, "[") {
			// Skip metadata or noise
			continue
		}

		if inTasks && (strings.HasPrefix(line, "-") || strings.HasPrefix(line, "Task")) {
			desc := line
			desc = strings.TrimPrefix(desc, "- [ ]")
			desc = strings.TrimPrefix(desc, "- [x]")
			desc = strings.TrimPrefix(desc, "-")
			tasks = append(tasks, Task{ID: id, Description: strings.TrimSpace(desc)})
			id++
		}
	}
	return tasks, scanner.Err()
}

func parseCompletedTasks(path string) (map[int]bool, error) {
	completed := make(map[int]bool)
	data, err := os.ReadFile(path)
	if err != nil { return completed, nil }

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "Task") && strings.Contains(line, "complete") {
			var id int
			_, err := fmt.Sscanf(strings.TrimSpace(line), "### Task %d complete", &id)
			if err == nil && id > 0 { completed[id] = true }
		}
	}
	return completed, nil
}

func MarkTaskDone(repoPath string, taskID int, description string) error {
	state, err := EnsureBranchState(repoPath, "")
	if err != nil { return err }
	execPath := filepath.Join(state.WipDir, "execution.md")
	note := fmt.Sprintf("\n### Task %d complete: %s\n- %s\n", taskID, description, util.NowISO())
	f, err := os.OpenFile(execPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil { return err }
	defer f.Close()
	_, err = f.WriteString(note)
	return err
}
