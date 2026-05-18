package bootstrap

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/conf/app"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/genprovider"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/genprovider/codex"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/genprovider/notebooklm"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/genprovider/simulated"
)

// ProviderRegistry resolves the configured generation adapter for each output
// type. Vendor selection and per-vendor configuration live here; the workflow
// only ever sees a genprovider.Provider. Satisfies generation.ProviderResolver.
type ProviderRegistry struct {
	providers map[generation.OutputType]map[string]genprovider.Provider
}

// NewProviderRegistry builds the registry from the resolved generation config.
// The catalog is static, but provider construction is host-local: adapters
// whose host requirements are absent stay registered as nil slots so jobs can
// fail terminally with PROVIDER_UNAVAILABLE instead of failing process startup.
func NewProviderRegistry(cfg app.GenerationConfig) (ProviderRegistry, error) {
	return ProviderRegistry{
		providers: map[generation.OutputType]map[string]genprovider.Provider{
			generation.OutputImage: imageProviders(),
			generation.OutputAudio: audioProviders(cfg),
		},
	}, nil
}

// PickForJob returns the named adapter for the job. The provider name is
// required: jobs must commit to a specific adapter at submit time (clients
// pick from ListGenerationModels). An empty or unknown name surfaces as a
// terminal PROVIDER_UNAVAILABLE so the FSM gives up instead of silently
// swapping to a different vendor.
func (r ProviderRegistry) PickForJob(o generation.OutputType, providerName string) (genprovider.Provider, error) {
	name := normalizeProviderName(providerName)
	if name == "" {
		return nil, generation.Terminal("PROVIDER_UNAVAILABLE", "provider not specified")
	}
	byName := r.providers[o]
	provider, ok := byName[name]
	if !ok || provider == nil {
		return nil, generation.Terminal("PROVIDER_UNAVAILABLE", fmt.Sprintf("%s not registered on this worker", name))
	}
	return provider, nil
}

func normalizeProviderName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func imageProviders() map[string]genprovider.Provider {
	out := map[string]genprovider.Provider{
		"simulated": simulated.New(),
		"codex":     nil,
	}
	if _, err := exec.LookPath("codex"); err == nil {
		p := codex.New()
		if v := strings.TrimSpace(os.Getenv("CODEX_TIMEOUT")); v != "" {
			if d, err := time.ParseDuration(v); err == nil && d > 0 {
				p.Timeout = d
			}
		}
		out["codex"] = p
	}
	return out
}

// GenerationModelInfo is one row in the catalog the operator console reads to
// render the submit form's model picker. The list of models is intentionally
// closed-set: free-text would let operators submit identifiers the configured
// provider does not understand. When a provider adds a new selectable model,
// extend the per-provider switch below.
type GenerationModelInfo struct {
	OutputType   string // "image" | "audio"
	Provider     string // "simulated" | "codex" | "notebooklm"
	Models       []string
	DefaultModel string
}

// GenerationCatalog returns the static catalog of defined provider/model pairs.
// Ordering is intentional — the first entry per output type is what the
// operator console pre-selects in the dropdown. Host capability is ignored
// here; workers reject unavailable providers at job time.
func GenerationCatalog() []GenerationModelInfo {
	return []GenerationModelInfo{
		imageCatalog("codex"),
		imageCatalog("simulated"),
		audioCatalog("notebooklm"),
		audioCatalog("simulated"),
	}
}

func imageCatalog(provider string) GenerationModelInfo {
	switch provider {
	case "codex":
		return GenerationModelInfo{
			OutputType:   "image",
			Provider:     "codex",
			Models:       []string{"gpt-5.5"},
			DefaultModel: "gpt-5.5",
		}
	default:
		return GenerationModelInfo{
			OutputType:   "image",
			Provider:     "simulated",
			Models:       []string{"simulated-v1"},
			DefaultModel: "simulated-v1",
		}
	}
}

func audioCatalog(provider string) GenerationModelInfo {
	switch provider {
	case "notebooklm":
		return GenerationModelInfo{
			OutputType:   "audio",
			Provider:     "notebooklm",
			Models:       []string{"notebooklm-default"},
			DefaultModel: "notebooklm-default",
		}
	default:
		return GenerationModelInfo{
			OutputType:   "audio",
			Provider:     "simulated",
			Models:       []string{"simulated-v1"},
			DefaultModel: "simulated-v1",
		}
	}
}

func audioProviders(cfg app.GenerationConfig) map[string]genprovider.Provider {
	out := map[string]genprovider.Provider{
		"simulated":  simulated.New(),
		"notebooklm": nil,
	}
	if p := constructNotebookLM(cfg); p != nil {
		out["notebooklm"] = p
	}
	return out
}

func constructNotebookLM(cfg app.GenerationConfig) genprovider.Provider {
	// NotebookLM paths default to $HOME-relative locations when blank;
	// the YAML can't express ~ portably so the fallback lives here.
	home, _ := os.UserHomeDir()
	script := cfg.NotebookLM.ScriptPath
	state := cfg.NotebookLM.StatePath
	if state == "" {
		state = filepath.Join(home, ".notebooklm", "state.json")
	}
	stateLabel := cfg.NotebookLM.StateDisplayPath
	pythonBin := cfg.NotebookLM.PythonBin
	if pythonBin == "" {
		pythonBin = filepath.Join(home, ".notebooklm", "venv", "bin", "python3")
	}
	if script == "" {
		return nil
	}
	if _, err := os.Stat(state); err != nil {
		return nil
	}
	if _, err := os.Stat(script); err != nil {
		return nil
	}
	p := notebooklm.New(pythonBin, script, state)
	p.StorageLabel = stateLabel
	return p
}
