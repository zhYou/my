package target

import "github.com/zhYou/my/internal/app/machine"

// Validate 在资源创建前校验目标选择和机器 ID 的本地约束。
func (o CaptureOptions) Validate(definitions []Definition) error {
	if (len(o.Targets) == 0) == !o.All {
		return invalidInput()
	}
	if err := validateSelection(o.Targets, definitions, o.All); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, id := range o.Machines {
		if machine.ValidateID(id) != nil || seen[id] {
			return invalidInput()
		}
		seen[id] = true
	}
	return nil
}

// Validate 校验显式目标选择；空选择表示所有已收录目标。
func (o DiffOptions) Validate(definitions []Definition) error {
	return validateSelection(o.Targets, definitions, true)
}

// Validate 校验显式目标选择；空选择表示所有已收录目标。
func (o SyncOptions) Validate(definitions []Definition) error {
	return validateSelection(o.Targets, definitions, true)
}

// Validate 校验目标、单位归属和显式来源。
func (o ResolveOptions) Validate(definitions []Definition) error {
	if err := validateSelection([]ID{o.Target}, definitions, false); err != nil {
		return err
	}
	if o.Unit.Validate() != nil || o.From != ResolveLocal && o.From != ResolveRemote {
		return invalidInput()
	}
	for _, definition := range definitions {
		if definition.ID != o.Target {
			continue
		}
		switch definition.Kind {
		case UnitSecret:
			if o.Unit == SecretUnitID {
				return nil
			}
		case UnitFile:
			for _, file := range definition.Files {
				if o.Unit == FileUnitID(file.Name) {
					return nil
				}
			}
		case UnitField:
			for _, goos := range []string{platformLinux, platformDarwin, platformWindows} {
				if _, ok := localTOMLPath(definition, o.Unit, goos); ok {
					return nil
				}
			}
		}
	}
	return invalidInput()
}

// Validate 校验非空、无重复的目标选择。
func (o RemoveOptions) Validate(definitions []Definition) error {
	return validateSelection(o.Targets, definitions, false)
}

func validateSelection(ids []ID, definitions []Definition, allowEmpty bool) error {
	if len(ids) == 0 && !allowEmpty {
		return invalidInput()
	}
	known := make(map[ID]Definition, len(definitions))
	for _, definition := range definitions {
		known[definition.ID] = definition
	}
	seen := map[ID]bool{}
	for _, id := range ids {
		definition, exists := known[id]
		if id.Validate() != nil || !exists || seen[id] || definition.RemoteAuthoritative {
			return invalidInput()
		}
		seen[id] = true
	}
	return nil
}
