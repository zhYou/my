package target

import "github.com/zhYou/my/internal/datarepo"

// clearOrphanBaselines 删除远端已不存在目标的本机基线，只修改本机状态。
func (s *Service) clearOrphanBaselines(
	metadata datarepo.Metadata,
	snapshot datarepo.Snapshot,
	progress *operation,
) ([]ID, error) {
	if !snapshot.ConfigInitialized {
		return nil, nil
	}
	remote, err := parseRemoteTargets(snapshot, s.definitions())
	if err != nil {
		return nil, err
	}
	removed := []ID{}
	for _, id := range sortedTargetIDs(s.definitions()) {
		if _, managed := remote[id]; managed {
			continue
		}
		_, exists, err := s.options.State.Read(metadata, id)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		if err := s.options.State.Clear(metadata, id); err != nil {
			return nil, err
		}
		progress.confirmed("target.baseline_removed:" + string(id))
		removed = append(removed, id)
	}
	return removed, nil
}
