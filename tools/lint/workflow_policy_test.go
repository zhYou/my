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

// workflowPolicyFixture 是需要保持同步的 CI workflow 与维护文档。
type workflowPolicyFixture struct {
	workflow    string
	maintenance string
}

func TestWorkflowPRSkipPolicy(t *testing.T) {
	fixture := loadWorkflowPolicyFixture(t)
	require.NoError(t, checkWorkflowPolicy([]byte(fixture.workflow), []byte(fixture.maintenance)))
}

func TestWorkflowPRSkipPolicyRejectsDrift(t *testing.T) {
	fixture := loadWorkflowPolicyFixture(t)
	tests := []struct {
		name   string
		want   string
		mutate func(workflowPolicyFixture) workflowPolicyFixture
	}{
		{
			name: "跳过键字面量被改写",
			want: "跳过例外字面量",
			mutate: func(fixture workflowPolicyFixture) workflowPolicyFixture {
				fixture.workflow = strings.Replace(
					fixture.workflow,
					`.key == "agent-mcp-platforms"`,
					`.key == "agent-mcp"`,
					1,
				)
				return fixture
			},
		},
		{
			name: "workflow 与文档同步改掉仍失败",
			want: "跳过例外字面量",
			mutate: func(fixture workflowPolicyFixture) workflowPolicyFixture {
				fixture.workflow = strings.Replace(
					fixture.workflow,
					`.key == "agent-mcp-platforms"`,
					`.key == "agent-mcp"`,
					1,
				)
				fixture.maintenance = strings.Replace(
					fixture.maintenance,
					"ci-allowed-pr-skips: agent-mcp-platforms",
					"ci-allowed-pr-skips: agent-mcp",
					1,
				)
				return fixture
			},
		},
		{
			name: "跳过例外移出汇总作业",
			want: "跳过例外位置",
			mutate: func(fixture workflowPolicyFixture) workflowPolicyFixture {
				fixture.workflow += `  rogue:
    runs-on: ubuntu-latest
    steps:
      - run: jq -e '.key == "agent-mcp-platforms" and .value.result == "skipped"'
`
				return fixture
			},
		},
		{
			name: "非 run 字段中的同义跳过表达式",
			want: "跳过例外位置",
			mutate: func(fixture workflowPolicyFixture) workflowPolicyFixture {
				fixture.workflow = strings.Replace(
					fixture.workflow,
					"      CI_CACHE_ROOT: ${{ github.workspace }}/../.ci",
					`      CI_CACHE_ROOT: jq -e '.value.result == "skipped"'`,
					1,
				)
				return fixture
			},
		},
		{
			name: "维护文档标记与 workflow 不一致",
			want: "维护文档",
			mutate: func(fixture workflowPolicyFixture) workflowPolicyFixture {
				fixture.maintenance = strings.Replace(
					fixture.maintenance,
					"ci-allowed-pr-skips: agent-mcp-platforms",
					"ci-allowed-pr-skips: agent-mcp",
					1,
				)
				return fixture
			},
		},
		{
			name: "新增作业未加入 needs",
			want: "check.needs",
			mutate: func(fixture workflowPolicyFixture) workflowPolicyFixture {
				fixture.workflow += `  docs-only:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`
				return fixture
			},
		},
		{
			name: "设计跳过条件被放宽",
			want: "非 PR",
			mutate: func(fixture workflowPolicyFixture) workflowPolicyFixture {
				fixture.workflow = strings.Replace(
					fixture.workflow,
					"    if: ${{ github.event_name != 'pull_request' }}",
					"    if: ${{ always() }}",
					1,
				)
				return fixture
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changed := tt.mutate(fixture)
			err := checkWorkflowPolicy([]byte(changed.workflow), []byte(changed.maintenance))
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func loadWorkflowPolicyFixture(t *testing.T) workflowPolicyFixture {
	t.Helper()
	t.Chdir(filepath.Join("..", ".."))
	workflow, err := os.ReadFile(filepath.Join(".github", "workflows", "ci.yml"))
	require.NoError(t, err)
	maintenance, err := os.ReadFile(filepath.Join("docs", "maintenance.md"))
	require.NoError(t, err)
	return workflowPolicyFixture{workflow: string(workflow), maintenance: string(maintenance)}
}

// ciWorkflow 是 CI 策略检查所需的 workflow 视图：按作业保留条件、needs 和全部标量值。
type ciWorkflow struct {
	jobs         map[string]ciJob
	otherScalars []string
}

type ciJob struct {
	ifCondition string
	needs       []string
	scalars     []string
}

func checkWorkflowPolicy(workflowYAML, maintenanceDoc []byte) error {
	workflow, err := parseCIWorkflow(workflowYAML)
	if err != nil {
		return err
	}
	check, ok := workflow.jobs[checkJob]
	if !ok {
		return fmt.Errorf("CI 汇总作业 %s 不存在", checkJob)
	}
	skipKeys, err := checkPRSkipAllowance(workflow)
	if err != nil {
		return err
	}
	if err := checkAggregateNeeds(workflow, check); err != nil {
		return err
	}
	if err := checkMaintenanceSkips(maintenanceDoc, skipKeys); err != nil {
		return err
	}
	return checkPlatformCondition(workflow)
}

// parseCIWorkflow 解析 workflow，并分别收集各作业与作业之外的标量值。
func parseCIWorkflow(workflowYAML []byte) (ciWorkflow, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(workflowYAML, &document); err != nil {
		return ciWorkflow{}, fmt.Errorf("解析 CI workflow：%w", err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return ciWorkflow{}, fmt.Errorf("CI workflow 顶层必须是映射")
	}
	root := document.Content[0]
	jobsNode := yamlMappingValue(root, "jobs")
	if jobsNode == nil || jobsNode.Kind != yaml.MappingNode {
		return ciWorkflow{}, fmt.Errorf("CI workflow 缺少 jobs 映射")
	}
	workflow := ciWorkflow{jobs: make(map[string]ciJob, len(jobsNode.Content)/2)}
	for i := 0; i+1 < len(jobsNode.Content); i += 2 {
		name := jobsNode.Content[i].Value
		jobNode := jobsNode.Content[i+1]
		workflow.jobs[name] = ciJob{
			ifCondition: yamlScalar(yamlMappingValue(jobNode, "if")),
			needs:       yamlScalarList(yamlMappingValue(jobNode, "needs")),
			scalars:     yamlScalarValues(jobNode),
		}
	}
	workflow.otherScalars = yamlScalarValuesOutside(root, "jobs")
	return workflow, nil
}

var (
	skipAllowancePattern = regexp.MustCompile(`\.value\.result\s*==\s*"skipped"`)
	skipKeyPattern       = regexp.MustCompile(
		`\.key\s*==\s*"([^"]+)"\s*and\s+\.value\.result\s*==\s*"skipped"`,
	)
)

// checkPRSkipAllowance 确认跳过表达式只出现一次且只属于 check 汇总作业，
// 并返回 workflow 中出现的例外键。
func checkPRSkipAllowance(workflow ciWorkflow) ([]string, error) {
	locations := make(map[string]int, len(workflow.jobs))
	var keys []string
	for name, job := range workflow.jobs {
		count := 0
		for _, scalar := range job.scalars {
			count += len(skipAllowancePattern.FindAllStringIndex(scalar, -1))
			for _, match := range skipKeyPattern.FindAllStringSubmatch(scalar, -1) {
				keys = append(keys, match[1])
			}
		}
		if count > 0 {
			locations[name] = count
		}
	}
	outside := 0
	for _, scalar := range workflow.otherScalars {
		outside += len(skipAllowancePattern.FindAllStringIndex(scalar, -1))
		for _, match := range skipKeyPattern.FindAllStringSubmatch(scalar, -1) {
			keys = append(keys, match[1])
		}
	}
	if outside > 0 {
		locations["jobs 之外"] = outside
	}
	var positioned []string
	for name := range locations {
		positioned = append(positioned, name)
	}
	slices.Sort(positioned)
	if len(positioned) != 1 || positioned[0] != checkJob {
		return nil, fmt.Errorf("PR 跳过例外位置必须仅在 %s 汇总作业，实际位于 %v", checkJob, positioned)
	}
	if locations[checkJob] != 1 {
		return nil, fmt.Errorf("PR 跳过例外位置必须在 %s 汇总作业中唯一出现，实际 %d 处", checkJob, locations[checkJob])
	}
	slices.Sort(keys)
	if len(keys) != 1 || keys[0] != allowedPRSkip {
		return nil, fmt.Errorf("PR 跳过例外字面量必须是 %s，实际为 %v", allowedPRSkip, keys)
	}
	return keys, nil
}

// checkAggregateNeeds 要求 check.needs 覆盖 workflow 中除汇总作业外的全部作业。
func checkAggregateNeeds(workflow ciWorkflow, check ciJob) error {
	expected := map[string]bool{}
	for name := range workflow.jobs {
		if name != checkJob {
			expected[name] = true
		}
	}
	actual := map[string]bool{}
	for _, name := range check.needs {
		actual[name] = true
	}
	var missing, unknown []string
	for name := range expected {
		if !actual[name] {
			missing = append(missing, name)
		}
	}
	for name := range actual {
		if !expected[name] {
			unknown = append(unknown, name)
		}
	}
	if len(missing) > 0 || len(unknown) > 0 {
		slices.Sort(missing)
		slices.Sort(unknown)
		return fmt.Errorf(
			"check.needs 必须覆盖 workflow 中除 %s 外的全部作业：缺少 %v，未知 %v",
			checkJob,
			missing,
			unknown,
		)
	}
	return nil
}

// checkMaintenanceSkips 要求维护文档标记与 workflow 跳过例外完全一致。
func checkMaintenanceSkips(maintenanceDoc []byte, workflowKeys []string) error {
	docPattern := regexp.MustCompile(`<!-- ci-allowed-pr-skips:\s*([^>]*?)\s*-->`)
	matches := docPattern.FindAllStringSubmatch(string(maintenanceDoc), -1)
	if len(matches) != 1 {
		return fmt.Errorf("维护文档必须包含唯一的 ci-allowed-pr-skips 标记，实际 %d 个", len(matches))
	}
	var documented []string
	for entry := range strings.SplitSeq(matches[0][1], ",") {
		if trimmed := strings.TrimSpace(entry); trimmed != "" {
			documented = append(documented, trimmed)
		}
	}
	slices.Sort(documented)
	if !slices.Equal(documented, workflowKeys) {
		return fmt.Errorf(
			"维护文档 ci-allowed-pr-skips 与 workflow 跳过例外不一致：文档 %v，workflow %v",
			documented,
			workflowKeys,
		)
	}
	return nil
}

// checkPlatformCondition 要求设计跳过的作业仍只在非 PR 运行。
func checkPlatformCondition(workflow ciWorkflow) error {
	platform, ok := workflow.jobs[allowedPRSkip]
	if !ok {
		return fmt.Errorf("CI 作业 %s 不存在，必须保留仅在非 PR 运行的设计", allowedPRSkip)
	}
	if strings.TrimSpace(platform.ifCondition) != "${{ github.event_name != 'pull_request' }}" {
		return fmt.Errorf("%s 必须仅在非 PR 运行，实际条件为 %q", allowedPRSkip, platform.ifCondition)
	}
	return nil
}

func yamlMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func yamlScalar(node *yaml.Node) string {
	if node == nil || node.Kind != yaml.ScalarNode {
		return ""
	}
	return node.Value
}

func yamlScalarList(node *yaml.Node) []string {
	if node == nil || node.Kind != yaml.SequenceNode {
		return nil
	}
	var values []string
	for _, item := range node.Content {
		if item.Kind == yaml.ScalarNode {
			values = append(values, item.Value)
		}
	}
	return values
}

// yamlScalarValues 按文档顺序收集节点下的全部标量值；映射只收集值，不收集键。
func yamlScalarValues(node *yaml.Node) []string {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.ScalarNode {
		return []string{node.Value}
	}
	var values []string
	switch node.Kind {
	case yaml.DocumentNode:
		for _, child := range node.Content {
			values = append(values, yamlScalarValues(child)...)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			values = append(values, yamlScalarValues(node.Content[i+1])...)
		}
	case yaml.SequenceNode:
		for _, child := range node.Content {
			values = append(values, yamlScalarValues(child)...)
		}
	}
	return values
}

// yamlScalarValuesOutside 收集映射中除 excludedKey 外各值的标量。
func yamlScalarValuesOutside(node *yaml.Node, excludedKey string) []string {
	if node == nil || node.Kind != yaml.MappingNode {
		return yamlScalarValues(node)
	}
	var values []string
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == excludedKey {
			continue
		}
		values = append(values, yamlScalarValues(node.Content[i+1])...)
	}
	return values
}
