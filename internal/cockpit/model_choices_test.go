package cockpit

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/acp"
)

func modelErrorCatalog() []string {
	var models []string
	for i := 0; i < 20; i++ {
		models = append(models, fmt.Sprintf("other/unrelated-%02d", i))
	}
	// Even an exact name match from another provider must rank below models
	// from the explicitly requested provider.
	models[0] = "other/model-typo"
	for i := 0; i < 12; i++ {
		models = append(models, fmt.Sprintf("target/model-%02d", i))
	}
	return models
}

func modelErrorSession(models []string) acp.NewSessionResult {
	option := acp.ConfigOption{ID: "model", CurrentValue: models[0]}
	for _, model := range models {
		option.Options = append(option.Options, acp.ConfigValue{Value: model})
	}
	return acp.NewSessionResult{ConfigOptions: []acp.ConfigOption{option}}
}

func assertModelErrorChoices(t *testing.T, err error, hint string) []string {
	t.Helper()
	if err == nil {
		t.Fatal("expected model refusal")
	}
	message := err.Error()
	_, list, ok := strings.Cut(message, "; available: ")
	if !ok {
		_, list, ok = strings.Cut(message, "; available models: ")
	}
	list, _, truncated := strings.Cut(list, "; … and ")
	choices := strings.Split(list, ", ")
	if !ok || !truncated || len(choices) != 10 || !strings.HasSuffix(message, "… and 22 more; "+hint) {
		t.Fatalf("expected 10 choices, omitted count and list hint: %v", err)
	}
	return choices
}

func TestModelErrorChoicesAcrossHarnesses(t *testing.T) {
	for _, harness := range []string{"opencode", "kimi", "codex", "agy"} {
		for _, requested := range []string{"target/model-typo", "missing/model-00"} {
			t.Run(harness+"/"+requested, func(t *testing.T) {
				models := modelErrorCatalog()
				before := append([]string(nil), models...)
				opts := StartOpts{Model: requested}
				var err error
				hint := "run `" + harness + " models` to list all"
				switch harness {
				case "opencode":
					err = validateOpencodeModel(opts.Model, models)
				case "kimi":
					hint = "run `kimi` and enter `/model` to list all"
					_, err = applyACPOpts(context.Background(), &fakeConfigurer{}, "sid", modelErrorSession(models), opts)
				case "codex":
					hint = "run `codex debug models` to list all"
					s := newFakeCodexServer(t)
					s.handle("model/list", func(wireMessage) any {
						var entries []map[string]string
						for _, model := range models {
							entries = append(entries, map[string]string{"model": model})
						}
						return map[string]any{"data": entries}
					})
					cause := errors.New("model unavailable")
					err = s.client.modelRequestError(context.Background(), requested, cause)
					if !errors.Is(err, cause) {
						t.Fatalf("lost underlying cause: %v", err)
					}
				case "agy":
					// Replace only `agy models`, without invoking a vendor CLI.
					b := &agyBackend{command: func(ctx context.Context, _ ...string) *exec.Cmd {
						return exec.CommandContext(ctx, "printf", strings.Join(models, "\tModel\n")+"\tModel\n")
					}}
					_, err = b.Start(context.Background(), opts)
				}
				choices := assertModelErrorChoices(t, err, hint)
				for _, choice := range choices {
					if !strings.HasPrefix(choice, "target/") {
						t.Errorf("same provider or similar model missing from shortlist: %v", choices)
					}
				}
				if requested == "missing/model-00" && choices[0] != "target/model-00" {
					t.Errorf("nearest model must come first: %v", choices)
				}
				if !reflect.DeepEqual(models, before) {
					t.Fatal("error formatting changed the live catalog")
				}
			})
		}
	}
}

func TestACPModelErrorChoicesOnConfigurationAndResume(t *testing.T) {
	models := modelErrorCatalog()
	requested := "target/model-00"
	for _, fault := range []string{"set-error", "unconfirmed", "effort-reset", "resume-reset"} {
		t.Run(fault, func(t *testing.T) {
			res := modelErrorSession(models)
			res.ConfigOptions = append(res.ConfigOptions, acp.ConfigOption{ID: "thinking", Category: "thought_level", Options: []acp.ConfigValue{{Value: "high"}}})
			changed := modelErrorSession(models)
			changed.ConfigOptions[0].CurrentValue = "wrong"
			changed.ConfigOptions = append(changed.ConfigOptions, acp.ConfigOption{ID: "thinking", CurrentValue: "high"})
			f := &fakeConfigurer{options: changed.ConfigOptions}
			opts := StartOpts{Model: requested}
			var err error
			switch fault {
			case "set-error":
				f.err = errors.New("cannot set model")
			case "effort-reset":
				res.ConfigOptions[0].CurrentValue = requested
				opts.Effort = "high"
				// The first update confirms the model; the effort update resets it.
				c := &modelChoicesReset{models: models, requested: requested}
				_, err = applyACPOpts(context.Background(), c, "sid", res, opts)
			case "resume-reset":
				_, err = applyACPResumeOpts(context.Background(), f, "sid", opts)
			}
			if fault == "set-error" || fault == "unconfirmed" {
				_, err = applyACPOpts(context.Background(), f, "sid", res, opts)
			}
			assertModelErrorChoices(t, err, "run `kimi` and enter `/model` to list all")
		})
	}
}

type modelChoicesReset struct {
	fakeConfigurer
	models    []string
	requested string
}

func (c *modelChoicesReset) SetConfigOption(_ context.Context, _ acp.SessionID, id, _ string) ([]acp.ConfigOption, error) {
	res := modelErrorSession(c.models)
	res.ConfigOptions[0].CurrentValue = c.requested
	if id == "thinking" {
		res.ConfigOptions[0].CurrentValue = "wrong"
	}
	return append(res.ConfigOptions, acp.ConfigOption{ID: "thinking", Category: "thought_level", CurrentValue: "high", Options: []acp.ConfigValue{{Value: "high"}}}), nil
}
