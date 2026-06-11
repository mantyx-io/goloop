package usercontext

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Store struct {
	Path string
}

func New(path string) *Store {
	return &Store{Path: path}
}

func (s *Store) Read() (string, error) {
	data, err := os.ReadFile(s.Path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func (s *Store) Append(iteration int, question, answer string) error {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}

	if _, err := os.Stat(s.Path); os.IsNotExist(err) {
		header := "# User Context\n\nAnswers provided by the human operator during the agentic loop.\n\n"
		if err := os.WriteFile(s.Path, []byte(header), 0o644); err != nil {
			return err
		}
	}

	now := time.Now().UTC().Format("2006-01-02 15:04 UTC")
	block := fmt.Sprintf("## %s — Iteration %d\n\n**Question:** %s\n\n**Answer:**\n%s\n\n",
		now, iteration, strings.TrimSpace(question), answer)

	f, err := os.OpenFile(s.Path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(block)
	return err
}
