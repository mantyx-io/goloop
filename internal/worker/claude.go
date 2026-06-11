package worker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"

	"github.com/mantyx-io/goloop/internal/agent"
	"github.com/mantyx-io/goloop/internal/config"
	"github.com/mantyx-io/goloop/internal/display"
)

type ClaudeCode struct {
	cfg     *config.Config
	display *display.Display
}

func NewClaudeCode(cfg *config.Config, disp *display.Display) *ClaudeCode {
	return &ClaudeCode{cfg: cfg, display: disp}
}

func (c *ClaudeCode) RunBuilder(task string) (Result, error) {
	return c.run(task, agent.RoleBuilder, false)
}

func (c *ClaudeCode) RunEvaluator(task string) (Result, error) {
	return c.run(task, agent.RoleEvaluator, true)
}

func (c *ClaudeCode) RunToolsmith(task string) (Result, error) {
	return c.run(task, agent.RoleToolsmith, false)
}

func (c *ClaudeCode) run(task string, role agent.Role, readOnly bool) (Result, error) {
	cmd := []string{
		c.cfg.ClaudeCodeBinary,
		"-p",
		"--model", c.cfg.ClaudeCodeModel,
		"--bare",
	}

	useStream := c.cfg.WorkerShowReasoning
	if useStream {
		cmd = append(cmd, "--output-format", "stream-json")
	} else {
		cmd = append(cmd, "--output-format", "text")
	}

	def := resolveAgent(c.cfg, role)
	prompt := agent.WithContext(def, task)
	if def.ApprovalMode == "plan" {
		readOnly = true
	}

	if readOnly {
		cmd = append(cmd, "--allowedTools", "Read,Grep,Glob")
	} else {
		cmd = append(cmd, "--dangerously-skip-permissions")
	}
	cmd = append(cmd, prompt)

	if useStream {
		return c.runStreaming(cmd)
	}
	return c.runCapture(cmd)
}

func (c *ClaudeCode) runCapture(cmd []string) (Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(c.cfg.WorkerTimeoutSeconds)*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, cmd[0], cmd[1:]...)
	command.Dir = c.cfg.ProjectRoot
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	err := command.Run()
	rc := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			rc = exitErr.ExitCode()
		} else if ctx.Err() == context.DeadlineExceeded {
			rc = 124
			stderr.WriteString("claude timed out")
		} else {
			return Result{}, err
		}
	}

	return Result{
		ReturnCode: rc,
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		Command:    cmd,
	}, nil
}

func (c *ClaudeCode) runStreaming(cmd []string) (Result, error) {
	if c.display != nil {
		c.display.BeginWorkerStream()
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(c.cfg.WorkerTimeoutSeconds)*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, cmd[0], cmd[1:]...)
	command.Dir = c.cfg.ProjectRoot

	stdout, err := command.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr

	if err := command.Start(); err != nil {
		return Result{}, err
	}

	var reasoningParts, assistantParts []string
	finalResult := ""
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		handleStreamEvent(event, c.display, &reasoningParts, &assistantParts, &finalResult)
	}

	waitErr := command.Wait()
	rc := 0
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			rc = exitErr.ExitCode()
		} else if ctx.Err() == context.DeadlineExceeded {
			rc = 124
			stderr.WriteString("claude timed out")
			_ = command.Process.Kill()
		} else {
			return Result{}, waitErr
		}
	}

	if c.display != nil {
		c.display.EndWorkerStream()
	}

	stdoutText := strings.TrimSpace(finalResult)
	if stdoutText == "" {
		stdoutText = extractAssistantText(assistantParts)
	}

	return Result{
		ReturnCode: rc,
		Stdout:     stdoutText,
		Stderr:     stderr.String(),
		Command:    cmd,
		Reasoning:  strings.Join(reasoningParts, ""),
	}, nil
}
