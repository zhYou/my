package target

import (
	"context"
	"slices"

	"github.com/zhYou/my/internal/datarepo"
)

// ListStatus 描述目标在远端和本机之间的可见状态。
type ListStatus string

const (
	// StatusManaged 表示远端已纳管且适用于当前机器。
	StatusManaged ListStatus = "managed"
	// StatusLocal 表示目标仅存在于当前机器。
	StatusLocal ListStatus = "local"
	// StatusAbsent 表示远端和本机都没有目标内容。
	StatusAbsent ListStatus = "absent"
	// StatusOutOfScope 表示远端已纳管但不适用于当前机器。
	StatusOutOfScope ListStatus = "out-of-scope"
)

// ListOptions 选择是否包含完全不存在于远端和本机的内建目标。
type ListOptions struct {
	All bool
}

// ListItem 是目标清单中的一条只读记录。
type ListItem struct {
	Target         ID         `json:"target"`
	Status         ListStatus `json:"status"`
	Scope          []string   `json:"scope"`
	BaselineCommit string     `json:"baseline_commit"`
	Units          int        `json:"units"`
}

// ListResult 是按目标 ID 稳定排序的只读清单。
type ListResult struct {
	Snapshot datarepo.Metadata `json:"snapshot"`
	Targets  []ListItem        `json:"targets"`
}

// List 汇总远端纳管状态、本机内容和上次成功同步提交，不写入任何一侧。
func (s *Service) List(ctx context.Context, input ListOptions) (result ListResult, err error) {
	ctx, progress := s.begin(ctx, "target.list")
	defer progress.finish(&err)
	if err := ctx.Err(); err != nil {
		return result, err
	}

	if err := s.validate(); err != nil {
		return result, err
	}
	progress.step(ctx, phaseListRead)
	snapshot, err := s.options.Repository.Read(ctx)
	if err != nil {
		return result, err
	}
	definitions := s.definitions()
	remote, err := parseRemoteTargets(snapshot, definitions)
	if err != nil {
		return result, err
	}
	result = ListResult{Snapshot: snapshot.Metadata, Targets: []ListItem{}}
	for _, id := range sortedTargetIDs(definitions) {
		definition := definitions[id]
		remoteTarget, managed := remote[id]
		item := ListItem{Target: id, Scope: []string{}}
		if managed {
			item.Scope = scopeValues(remoteTarget.scope)
			item.Units = len(remoteTarget.units)
			if remoteTarget.scope.Applies(s.options.MachineID) {
				item.Status = StatusManaged
			} else {
				item.Status = StatusOutOfScope
			}
		} else {
			localExists, err := s.localTargetExists(definition)
			if err != nil {
				return result, err
			}
			if localExists {
				item.Status = StatusLocal
			} else {
				item.Status = StatusAbsent
			}
		}
		if item.Status == StatusAbsent && !input.All {
			continue
		}
		baseline, hasBaseline, err := s.options.State.Read(snapshot.Metadata, id)
		if err != nil {
			return result, err
		}
		if hasBaseline {
			item.BaselineCommit = baseline.Commit
		}
		result.Targets = append(result.Targets, item)
	}
	return result, nil
}

func (s *Service) localTargetExists(definition Definition) (bool, error) {
	if definition.RemoteAuthoritative {
		return false, nil
	}
	local, err := definition.ReadLocal(s.options.Home, s.options.OS)
	if err != nil {
		if isNotExist(err) {
			return false, nil
		}
		return false, err
	}
	for _, value := range local.Units {
		if value.Exists {
			return true, nil
		}
	}
	return false, nil
}

func scopeValues(scope MachineScope) []string {
	if scope.All {
		return []string{"*"}
	}
	values := append([]string(nil), scope.IDs...)
	if len(values) == 0 {
		return []string{}
	}
	slices.Sort(values)
	return values
}
