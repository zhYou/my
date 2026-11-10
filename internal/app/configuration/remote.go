package configuration

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/zhYou/my/internal/datarepo"
)

const (
	configManifestFile = "configs/manifest.toml"
	configTargetsRoot  = "configs/targets"
	fieldValuesFile    = "values.toml"
	targetMetadataFile = "target.toml"
)

type remoteTarget struct {
	id        TargetID
	scope     MachineScope
	units     map[UnitID]UnitValue
	files     map[string][]byte
	target    TargetDefinition
	version   string
	recipient string
}

type remoteMetadata struct {
	SchemaVersion int      `toml:"schema_version"`
	Target        TargetID `toml:"target"`
	Machines      []string `toml:"machines"`
	Version       string   `toml:"version,omitempty"`
	Recipient     string   `toml:"recipient,omitempty"`
}

func parseRemoteTargets(
	snapshot datarepo.Snapshot,
	definitions map[TargetID]TargetDefinition,
) (map[TargetID]remoteTarget, error) {
	result := map[TargetID]remoteTarget{}
	if !snapshot.ConfigInitialized {
		return result, nil
	}
	for name := range snapshot.Files {
		if !strings.HasPrefix(name, configTargetsRoot+"/") {
			continue
		}
		parts := strings.Split(name, "/")
		if len(parts) < 4 {
			return nil, problem(codeStateInvalid, phaseStateRead, nil)
		}
		id := TargetID(parts[2])
		if _, known := definitions[id]; !known {
			return nil, problem(codeStateInvalid, phaseStateRead, nil)
		}
	}
	for _, id := range sortedTargetIDs(definitions) {
		definition := definitions[id]
		prefix := remoteTargetPrefix(id)
		metadataData, exists := snapshot.Files[path.Join(prefix, targetMetadataFile)]
		if !exists {
			continue
		}
		var metadata remoteMetadata
		decoder := toml.NewDecoder(bytes.NewReader(metadataData))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&metadata); err != nil ||
			metadata.SchemaVersion != 1 ||
			metadata.Target != id {
			return nil, problem(codeStateInvalid, phaseStateRead, err)
		}
		scope, err := parseScope(metadata.Machines)
		if err != nil {
			return nil, err
		}
		target := remoteTarget{
			id:        id,
			scope:     scope,
			units:     map[UnitID]UnitValue{},
			files:     map[string][]byte{},
			target:    definition,
			version:   metadata.Version,
			recipient: metadata.Recipient,
		}
		switch {
		case definition.RemoteAuthoritative:
			continue
		case definition.Kind == UnitSecret:
			if metadata.Version == "" || metadata.Recipient == "" || len(definition.Files) != 1 {
				return nil, problem(codeStateInvalid, phaseStateRead, nil)
			}
			file := definition.Files[0]
			data, exists := snapshot.Files[path.Join(prefix, configDataDirectory, file.Name+".age")]
			if !exists {
				return nil, problem(codeStateInvalid, phaseStateRead, nil)
			}
			target.units[SecretUnitID] = UnitValue{Exists: true, Digest: metadata.Version}
			target.files[file.Name] = data
		case definition.Kind == UnitField:
			valuesPath := path.Join(prefix, configDataDirectory, fieldValuesFile)
			if data, ok := snapshot.Files[valuesPath]; ok {
				units, err := decodeFieldValues(data)
				if err != nil {
					return nil, err
				}
				target.units = units
				target.files[fieldValuesFile] = data
			}
		default:
			for _, file := range definition.Files {
				name := path.Join(prefix, configDataDirectory, file.Name)
				data, ok := snapshot.Files[name]
				if !ok {
					continue
				}
				target.units[FileUnitID(file.Name)] = UnitValueFromBytes(data)
				target.files[file.Name] = data
			}
		}
		result[id] = target
	}
	return result, nil
}

func parseScope(values []string) (MachineScope, error) {
	if len(values) == 1 && values[0] == "*" {
		return MachineScope{All: true}, nil
	}
	if len(values) == 0 {
		return MachineScope{}, problem(codeStateInvalid, phaseStateRead, nil)
	}
	seen := map[string]struct{}{}
	for _, id := range values {
		if id == "*" || id == "" {
			return MachineScope{}, problem(codeStateInvalid, phaseStateRead, nil)
		}
		if _, duplicate := seen[id]; duplicate {
			return MachineScope{}, problem(codeStateInvalid, phaseStateRead, nil)
		}
		seen[id] = struct{}{}
	}
	return MachineScope{IDs: append([]string(nil), values...)}, nil
}

func encodeScope(scope MachineScope) ([]string, error) {
	if scope.All {
		if len(scope.IDs) != 0 {
			return nil, invalidInput()
		}
		return []string{"*"}, nil
	}
	if len(scope.IDs) == 0 {
		return nil, invalidInput()
	}
	return append([]string(nil), scope.IDs...), nil
}

func encodeRemoteMetadata(target TargetID, scope MachineScope) ([]byte, error) {
	return encodeRemoteMetadataWithSecret(target, scope, "", "")
}

func encodeRemoteMetadataWithSecret(
	target TargetID,
	scope MachineScope,
	version string,
	recipient string,
) ([]byte, error) {
	machines, err := encodeScope(scope)
	if err != nil {
		return nil, err
	}
	return toml.Marshal(remoteMetadata{
		SchemaVersion: 1,
		Target:        target,
		Machines:      machines,
		Version:       version,
		Recipient:     recipient,
	})
}

func decodeFieldValues(data []byte) (map[UnitID]UnitValue, error) {
	var document struct {
		SchemaVersion int            `toml:"schema_version"`
		Units         map[string]any `toml:"units"`
	}
	if err := toml.Unmarshal(data, &document); err != nil {
		return nil, invalidTOML(err)
	}
	if document.SchemaVersion != 1 {
		return nil, problem(codeStateInvalid, phaseStateRead, nil)
	}
	result := make(map[UnitID]UnitValue, len(document.Units))
	for name := range document.Units {
		id := UnitID(name)
		if err := id.Validate(); err != nil {
			return nil, err
		}
		value, err := TOMLValue(data, []string{"units", name})
		if err != nil {
			return nil, err
		}
		result[id] = value
	}
	return result, nil
}

func encodeFieldValues(units map[UnitID]UnitValue) ([]byte, error) {
	values := map[string]any{}
	for id, value := range units {
		if !value.Exists {
			continue
		}
		var decoded any
		if err := json.Unmarshal(value.Data, &decoded); err != nil {
			return nil, problem(codeStateInvalid, phaseStateWrite, err)
		}
		values[string(id)] = decoded
	}
	return toml.Marshal(struct {
		SchemaVersion int            `toml:"schema_version"`
		Units         map[string]any `toml:"units"`
	}{
		SchemaVersion: 1,
		Units:         values,
	})
}

func remoteFilesForTarget(target remoteTarget) (map[string][]byte, error) {
	prefix := remoteTargetPrefix(target.id)
	metadata, err := encodeRemoteMetadata(target.id, target.scope)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{
		path.Join(prefix, targetMetadataFile): metadata,
	}
	if target.target.Kind == UnitField {
		values, err := encodeFieldValues(target.units)
		if err != nil {
			return nil, err
		}
		files[path.Join(prefix, configDataDirectory, fieldValuesFile)] = values
		return files, nil
	}
	if target.target.Kind == UnitSecret {
		if target.version == "" || target.recipient == "" || len(target.target.Files) != 1 {
			return nil, problem(codeStateInvalid, phaseStateWrite, nil)
		}
		metadata, err := encodeRemoteMetadataWithSecret(
			target.id,
			target.scope,
			target.version,
			target.recipient,
		)
		if err != nil {
			return nil, err
		}
		files[path.Join(prefix, targetMetadataFile)] = metadata
		name := target.target.Files[0].Name
		ciphertext, exists := target.files[name]
		if !exists || len(ciphertext) == 0 {
			return nil, problem(codeStateInvalid, phaseStateWrite, nil)
		}
		files[path.Join(prefix, configDataDirectory, name+".age")] = append([]byte(nil), ciphertext...)
		return files, nil
	}
	for _, file := range target.target.Files {
		value, exists := target.units[FileUnitID(file.Name)]
		if !exists || !value.Exists {
			continue
		}
		files[path.Join(prefix, configDataDirectory, file.Name)] = append([]byte(nil), value.Data...)
	}
	return files, nil
}

func remoteTargetPrefix(target TargetID) string {
	return path.Join(configTargetsRoot, string(target))
}

func targetPaths(snapshot datarepo.Snapshot, target TargetID) []string {
	prefix := remoteTargetPrefix(target) + "/"
	result := make([]string, 0)
	for name := range snapshot.Files {
		if strings.HasPrefix(name, prefix) {
			result = append(result, name)
		}
	}
	sort.Strings(result)
	return result
}

func sortedTargetIDs(definitions map[TargetID]TargetDefinition) []TargetID {
	result := make([]TargetID, 0, len(definitions))
	for id := range definitions {
		result = append(result, id)
	}
	slices.Sort(result)
	return result
}

func targetFileByName(definition TargetDefinition, name string) (TargetFile, bool) {
	for _, file := range definition.Files {
		if file.Name == name {
			return file, true
		}
	}
	return TargetFile{}, false
}

func localFilePath(home string, file TargetFile) string {
	return filepath.Join(home, filepath.FromSlash(file.RelativePath))
}

func isNotExist(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}

func cloneUnitValues(values map[UnitID]UnitValue) map[UnitID]UnitValue {
	result := make(map[UnitID]UnitValue, len(values))
	for id, value := range values {
		result[id] = UnitValue{
			Exists: value.Exists,
			Digest: value.Digest,
			Data:   append([]byte(nil), value.Data...),
		}
	}
	return result
}

func unitOverlay(base, overlay map[UnitID]UnitValue) map[UnitID]UnitValue {
	result := cloneUnitValues(base)
	for id, value := range overlay {
		result[id] = UnitValue{
			Exists: value.Exists,
			Digest: value.Digest,
			Data:   append([]byte(nil), value.Data...),
		}
	}
	return result
}
