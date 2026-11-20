package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAgentMCPPlatformsIsOnlyAllowedPRSkip(t *testing.T) {
	t.Chdir(filepath.Join("..", ".."))
	workflow, err := os.ReadFile(filepath.Join(".github", "workflows", "ci.yml"))
	require.NoError(t, err)
	maintenance, err := os.ReadFile(filepath.Join("docs", "maintenance.md"))
	require.NoError(t, err)

	workflowText := string(workflow)
	require.Regexp(
		t,
		`(?ms)^  agent-mcp-platforms:\n(?:    .*\n)*?    if: \$\{\{ github\.event_name != 'pull_request' \}\}`,
		workflowText,
	)
	needs := regexp.MustCompile(`(?m)^    needs: \[([^\]]+)\]`).FindStringSubmatch(workflowText)
	require.Len(t, needs, 2)
	var jobs []string
	for job := range strings.SplitSeq(needs[1], ",") {
		jobs = append(jobs, strings.TrimSpace(job))
	}
	slices.Sort(jobs)
	require.Equal(t, []string{
		"agent-mcp",
		"agent-mcp-platforms",
		"fuzz-benchmark",
		"package",
		"platform-tests",
		"platforms",
		"race",
		"source",
	}, jobs)

	skipPattern := regexp.MustCompile(`\.key == "([^"]+)" and \.value\.result == "skipped"`)
	workflowSkips := skipPattern.FindAllStringSubmatch(workflowText, -1)
	require.Len(t, workflowSkips, 1)
	require.Equal(t, 1, strings.Count(workflowText, `.value.result == "skipped"`))

	docPattern := regexp.MustCompile(`<!-- ci-allowed-pr-skips: ([^>]+) -->`)
	docSkips := docPattern.FindStringSubmatch(string(maintenance))
	require.Len(t, docSkips, 2)
	var documented []string
	for job := range strings.SplitSeq(docSkips[1], ",") {
		documented = append(documented, strings.TrimSpace(job))
	}
	require.Equal(t, []string{workflowSkips[0][1]}, documented)
}
