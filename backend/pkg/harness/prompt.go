package harness

import (
	"pentagi/pkg/docker"
	"pentagi/pkg/templates"
	"pentagi/pkg/tools"
)

func BuildSystemPrompt(prompter templates.Prompter, targetInput, image string, coverageState *tools.CoverageState) (string, error) {
	coverageSummary := ""
	authSummary := ""
	if coverageState != nil {
		coverageSummary = coverageState.GetCoverageSummary()
	}

	return prompter.RenderTemplate(templates.PromptTypePhaseExecutor, map[string]any{
		"TargetURL":       targetInput,
		"DockerImage":     image,
		"Cwd":             docker.WorkFolderPathInContainer,
		"CoverageSummary": coverageSummary,
		"AuthSummary":     authSummary,
	})
}
