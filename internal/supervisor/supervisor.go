package supervisor

import (
	"fmt"

	"github.com/mantyx-io/goloop/internal/config"
	"github.com/mantyx-io/goloop/internal/llm"
)

func New(cfg *config.Config) (llm.Client, error) {
	switch cfg.SupervisorBackend {
	case config.SupervisorOpenAI:
		return llm.NewOpenAI(
			cfg.SupervisorModel,
			cfg.SupervisorAPIKey,
			cfg.SupervisorBaseURL,
			cfg.SupervisorTemperature,
		)
	case config.SupervisorChatGPT:
		return llm.NewChatGPT(cfg.SupervisorModel, cfg.SupervisorAuthPath)
	case config.SupervisorAnthropic:
		return llm.NewAnthropic(
			cfg.SupervisorModel,
			cfg.SupervisorAPIKey,
			cfg.SupervisorBaseURL,
			cfg.SupervisorTemperature,
		)
	default:
		return nil, fmt.Errorf("unsupported supervisor backend: %s", cfg.SupervisorBackend)
	}
}
