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

type Cursor struct {
	cfg     *config.Config
	display *display.Display
}

func NewCursor(cfg *config.Config, disp *display.Display) *Cursor {
	return &Cursor{cfg: cfg, display: disp}
}

func (c *Cursor) RunBuilder(ctx context.Context, task string) (Result, error) {
	return c.run(ctx, task, agent.RoleBuilder, false)
}

func (c *Cursor) RunEvaluator(ctx context.Context, task string) (Result, error) {
	return c.run(ctx, task, agent.RoleEvaluator, true)
}

func (c *Cursor) RunToolsmith(ctx context.Context, task string) (Result, error) {
	return c.run(ctx, task, agent.RoleToolsmith, false)
}

func (c *Cursor) run(ctx context.Context, task string, role agent.Role, readOnly bool) (Result, error) {
	cmd := []string{
		c.cfg.CursorBinary,
		"agent", "-p",
		"--model", c.cfg.CursorModel,
		"--workspace", c.cfg.ProjectRoot,
		"--trust",
	}

	useStream := c.cfg.WorkerShowReasoning
	if useStream {
		cmd = append(cmd, "--output-format", "stream-json", "--stream-partial-output")
	} else {
		cmd = append(cmd, "--output-format", "text")
	}

	def := resolveAgent(c.cfg, role)
	prompt := agent.WithContext(def, task)
	if def.ApprovalMode == "plan" {
		readOnly = true
	}

	if readOnly {
		cmd = append(cmd, "--plan")
	} else {
		cmd = append(cmd, "--yolo")
	}
	cmd = append(cmd, prompt)

	if useStream {
		return c.runStreaming(ctx, cmd)
	}
	return c.runCapture(ctx, cmd)
}

func (c *Cursor) runCapture(ctx context.Context, cmd []string) (Result, error) {
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(c.cfg.WorkerTimeoutSeconds)*time.Second)
	defer cancel()

	command := exec.CommandContext(runCtx, cmd[0], cmd[1:]...)
	command.Dir = c.cfg.ProjectRoot
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	err := command.Run()
	rc := 0
	if err != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			rc = 124
			stderr.WriteString("cursor agent timed out")
		} else if ctx.Err() != nil {
			return Result{}, ctx.Err()
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			rc = exitErr.ExitCode()
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

func (c *Cursor) runStreaming(ctx context.Context, cmd []string) (Result, error) {
	if c.display != nil {
		c.display.BeginWorkerStream()
	}

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(c.cfg.WorkerTimeoutSeconds)*time.Second)
	defer cancel()

	command := exec.CommandContext(runCtx, cmd[0], cmd[1:]...)
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
		if runCtx.Err() == context.DeadlineExceeded {
			rc = 124
			stderr.WriteString("cursor agent timed out")
		} else if ctx.Err() != nil {
			return Result{}, ctx.Err()
		} else if exitErr, ok := waitErr.(*exec.ExitError); ok {
			rc = exitErr.ExitCode()
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

func handleStreamEvent(
	event map[string]any,
	disp *display.Display,
	reasoningParts, assistantParts *[]string,
	finalResult *string,
) {
	eventType, _ := event["type"].(string)

	switch eventType {
	case "thinking":
		if subtype, _ := event["subtype"].(string); subtype == "delta" {
			text, _ := event["text"].(string)
			if text != "" {
				*reasoningParts = append(*reasoningParts, text)
				if disp != nil {
					disp.WorkerThinkingDelta(text)
				}
			}
		}
	case "assistant":
		text := messageText(event["message"])
		if text == "" {
			return
		}
		var delta string
		if len(*assistantParts) > 0 && strings.HasPrefix(text, (*assistantParts)[len(*assistantParts)-1]) {
			delta = text[len((*assistantParts)[len(*assistantParts)-1]):]
			(*assistantParts)[len(*assistantParts)-1] = text
		} else if len(*assistantParts) == 0 || len(text) >= len((*assistantParts)[len(*assistantParts)-1]) {
			delta = text
			if len(*assistantParts) > 0 {
				(*assistantParts)[len(*assistantParts)-1] = text
			} else {
				*assistantParts = append(*assistantParts, text)
			}
		}
		if delta != "" && disp != nil {
			disp.WorkerAssistantDelta(delta)
		}
	case "result":
		if result, ok := event["result"].(string); ok {
			*finalResult = result
		}
	}
}

func messageText(raw any) string {
	msg, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	content := msg["content"]
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, block := range v {
			b, ok := block.(map[string]any)
			if !ok {
				continue
			}
			if b["type"] == "text" {
				if text, ok := b["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "")
	}
	return ""
}

func extractAssistantText(chunks []string) string {
	if len(chunks) == 0 {
		return ""
	}
	longest := chunks[0]
	for _, c := range chunks[1:] {
		if len(c) > len(longest) {
			longest = c
		}
	}
	return strings.TrimSpace(longest)
}
