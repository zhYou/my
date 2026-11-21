package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// allowedPRSkip 是 CI 汇总作业在 PR 上唯一允许跳过的作业。
const allowedPRSkip = "agent-mcp-platforms"

// checkJob 是汇总全部 CI 作业的检查入口。
const checkJob = "check"

func TestWorkflowPRSkipPolicy(t *testing.T) {
	t.Chdir(filepath.Join("..", ".."))
	workflow, err := os.ReadFile(filepath.Join(".github", "workflows", "ci.yml"))
	require.NoError(t, err)
	maintenance, err := os.ReadFile(filepath.Join("docs", "maintenance.md"))
	require.NoError(t, err)
	require.NoError(t, checkWorkflowPolicy(workflow, maintenance))
}

func TestWorkflowPRSkipPolicyRejectsDrift(t *testing.T) {
	t.Chdir(filepath.Join("..", ".."))
	workflow, err := os.ReadFile(filepath.Join(".github", "workflows", "ci.yml"))
	require.NoError(t, err)
	maintenance, err := os.ReadFile(filepath.Join("docs", "maintenance.md"))
	require.NoError(t, err)

	tests := []struct {
		name   string
		want   string
		mutate func(workflow, maintenance string) (string, string)
	}{
		{
			name: "跳过键字面量被改写",
			want: "跳过例外字面量",
			mutate: func(workflow, maintenance string) (string, string) {
				return strings.Replace(workflow, `.key == "agent-mcp-platforms"`, `.key == "agent-mcp"`, 1), maintenance
			},
		},
		{
			name: "workflow 与文档同步改掉仍失败",
			want: "跳过例外字面量",
			mutate: func(workflow, maintenance string) (string, string) {
				return strings.Replace(workflow, `.key == "agent-mcp-platforms"`, `.key == "agent-mcp"`, 1),
					strings.Replace(
						maintenance,
						"ci-allowed-pr-skips: agent-mcp-platforms",
						"ci-allowed-pr-skips: agent-mcp",
						1,
					)
			},
		},
		{
			name: "跳过例外移出汇总作业",
			want: "跳过例外位置",
			mutate: func(workflow, maintenance string) (string, string) {
				return workflow + `  rogue:
    runs-on: ubuntu-latest
    steps:
      - run: jq -e '.key == "agent-mcp-platforms" and .value.result == "skipped"'
`, maintenance
			},
		},
		{
			name: "新增作业未加入 needs",
			want: "check.needs",
			mutate: func(workflow, maintenance string) (string, string) {
				return workflow + `  docs-only:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`, maintenance
			},
		},
		{
			name: "维护文档标记与 workflow 不一致",
			want: "维护文档",
			mutate: func(workflow, maintenance string) (string, string) {
				return workflow, strings.Replace(
					maintenance,
					"ci-allowed-pr-skips: agent-mcp-platforms",
					"ci-allowed-pr-skips: agent-mcp",
					1,
				)
			},
		},
		{
			name: "设计跳过条件被放宽",
			want: "非 PR",
			mutate: func(workflow, maintenance string) (string, string) {
				return strings.Replace(
					workflow,
					"    if: ${{ github.event_name != 'pull_request' }}",
					"    if: ${{ always() }}",
					1,
				), maintenance
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changedWorkflow, changedMaintenance := tt.mutate(string(workflow), string(maintenance))
			err := checkWorkflowPolicy([]byte(changedWorkflow), []byte(changedMaintenance))
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func checkWorkflowPolicy(workflowYAML, maintenanceDoc []byte) error {
	var workflow struct {
		Jobs map[string]struct {
			If    string   `yaml:"if"`
			Needs []string `yaml:"needs"`
			Steps []struct {
				Run string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(workflowYAML, &workflow); err != nil {
		return fmt.Errorf("解析 CI workflow：%w", err)
	}
	check, ok := workflow.Jobs[checkJob]
	if !ok {
		return fmt.Errorf("CI 汇总作业 %s 不存在", checkJob)
	}

	skipAllowance := regexp.MustCompile(`\.value\.result\s*==\s*"skipped"`)
	skipKey := regexp.MustCompile(`\.key\s*==\s*"([^"]+)"\s*and\s*\.value\.result\s*==\s*"skipped"`)
	locations := map[string]int{}
	var keys []string
	for name, job := range workflow.Jobs {
		for _, step := range job.Steps {
			count := len(skipAllowance.FindAllStringIndex(step.Run, -1))
			if count == 0 {
				continue
			}
			locations[name] += count
			for _, match := range skipKey.FindAllStringSubmatch(step.Run, -1) {
				keys = append(keys, match[1])
			}
		}
	}
	var positioned []string
	for name := range locations {
		positioned = append(positioned, name)
	}
	slices.Sort(positioned)
	if len(positioned) != 1 || positioned[0] != checkJob {
		return fmt.Errorf("PR 跳过例外位置必须仅在 %s 汇总作业，实际位于 %v", checkJob, positioned)
	}
	if locations[checkJob] != 1 {
		return fmt.Errorf("PR 跳过例外位置必须在 %s 汇总作业中唯一出现，实际 %d 处", checkJob, locations[checkJob])
	}
	if len(keys) != 1 || keys[0] != allowedPRSkip {
		return fmt.Errorf("PR 跳过例外字面量必须是 %s，实际为 %v", allowedPRSkip, keys)
	}

	expectedNeeds := map[string]bool{}
	for name := range workflow.Jobs {
		if name != checkJob {
			expectedNeeds[name] = true
		}
	}
	actualNeeds := map[string]bool{}
	for _, name := range check.Needs {
		actualNeeds[name] = true
	}
	var missing, unknown []string
	for name := range expectedNeeds {
		if !actualNeeds[name] {
			missing = append(missing, name)
		}
	}
	for name := range actualNeeds {
		if !expectedNeeds[name] {
			unknown = append(unknown, name)
		}
	}
	if len(missing) > 0 || len(unknown) > 0 {
		slices.Sort(missing)
		slices.Sort(unknown)
		return fmt.Errorf("check.needs 必须覆盖 workflow 中除 %s 外的全部作业：缺少 %v，未知 %v", checkJob, missing, unknown)
	}

	docPattern := regexp.MustCompile(`<!-- ci-allowed-pr-skips:\s*([^>]*?)\s*-->`)
	docMatches := docPattern.FindAllStringSubmatch(string(maintenanceDoc), -1)
	if len(docMatches) != 1 {
		return fmt.Errorf("维护文档必须包含唯一的 ci-allowed-pr-skips 标记，实际 %d 个", len(docMatches))
	}
	var documented []string
	for entry := range strings.SplitSeq(docMatches[0][1], ",") {
		if trimmed := strings.TrimSpace(entry); trimmed != "" {
			documented = append(documented, trimmed)
		}
	}
	slices.Sort(documented)
	if !slices.Equal(documented, keys) {
		return fmt.Errorf("维护文档 ci-allowed-pr-skips 与 workflow 跳过例外不一致：文档 %v，workflow %v", documented, keys)
	}

	platform, ok := workflow.Jobs[allowedPRSkip]
	if !ok {
		return fmt.Errorf("CI 作业 %s 不存在，必须保留仅在非 PR 运行的设计", allowedPRSkip)
	}
	if strings.TrimSpace(platform.If) != "${{ github.event_name != 'pull_request' }}" {
		return fmt.Errorf("%s 必须仅在非 PR 运行，实际条件为 %q", allowedPRSkip, platform.If)
	}
	return nil
}
