package shellenv

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/zhYou/my/internal/app/machine"
	"github.com/zhYou/my/internal/base/agecrypt"
	"github.com/zhYou/my/internal/base/logging"
	"github.com/zhYou/my/internal/base/messages"
	"github.com/zhYou/my/internal/datarepo"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Repository 是环境变量数据仓的 CAS 边界。
type Repository interface {
	Read(context.Context) (datarepo.Snapshot, error)
	CommitChanges(context.Context, datarepo.Snapshot, datarepo.Changes) (datarepo.Metadata, error)
}

// Options 显式注入环境业务依赖。
type Options struct {
	Logger         *logging.Logger
	Tracer         trace.Tracer
	Localizer      *messages.Localizer
	Repository     Repository
	Cache          Cache
	AgeKey         *agecrypt.Key
	LoadAgeKey     func() (*agecrypt.Key, error)
	MachineID      string
	KnownMachines  []string
	RepositoryID   int64
	RepositoryName string
	Branch         string
}

// Service 维护环境变量并渲染离线 shell 代码。
type Service struct {
	options Options
}

// SetOptions 声明一个变量写入。
type SetOptions struct {
	Name      string
	Secret    bool
	Value     []byte
	Machines  []string
	Overwrite bool
}

// RemoveOptions 声明一个变量删除。
type RemoveOptions struct {
	Name   string
	Secret bool
}

// Result 是环境变量写操作结果。
type Result struct {
	Snapshot datarepo.Metadata `json:"snapshot"`
	Name     string            `json:"name"`
	Secret   bool              `json:"secret"`
	Machines []string          `json:"machines"`
	Changed  bool              `json:"changed"`
}

// Variable 是安全列表记录，不包含值。
type Variable struct {
	Name     string   `json:"name"`
	Secret   bool     `json:"secret"`
	Machines []string `json:"machines"`
}

// ListResult 是环境变量名称清单。
type ListResult struct {
	Snapshot  datarepo.Metadata `json:"snapshot"`
	Variables []Variable        `json:"variables"`
}

type record struct {
	Value    string   `toml:"value"`
	Machines []string `toml:"machines"`
}

type recordsDocument struct {
	SchemaVersion int               `toml:"schema_version"`
	Env           map[string]record `toml:"env"`
}

type targetMetadata struct {
	SchemaVersion int      `toml:"schema_version"`
	Target        string   `toml:"target"`
	Machines      []string `toml:"machines"`
	Version       string   `toml:"version,omitempty"`
	Recipient     string   `toml:"recipient,omitempty"`
}

type repositoryState struct {
	ordinary         map[string]record
	secret           map[string]record
	ordinaryData     []byte
	secretCiphertext []byte
	secretVersion    string
	secretRecipient  string
}

// New 构造环境变量服务。
func New(options Options) *Service {
	if options.Logger == nil {
		options.Logger = logging.NewNop()
	}
	if options.Tracer == nil {
		options.Tracer = noop.NewTracerProvider().Tracer("my/shellenv")
	}
	return &Service{options: options}
}

// Set 在线写入一个普通或秘密变量并刷新本机缓存。
func (s *Service) Set(ctx context.Context, input SetOptions) (result Result, err error) {
	ctx, progress := s.begin(ctx, "shellenv.set")
	defer progress.finish(&err)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	progress.step(ctx, phaseRead)

	if err := input.Validate(); err != nil {
		return result, err
	}
	if len(input.Machines) != 0 {
		if err := (machineScope{IDs: input.Machines}).validate(s.options.KnownMachines); err != nil {
			return result, err
		}
	}
	if s.options.Repository == nil || s.options.Cache == nil {
		return result, invalidInput()
	}
	unlock, err := s.options.Cache.Lock(ctx)
	if err != nil {
		return result, err
	}
	defer func() {
		err = errors.Join(err, unlock())
	}()
	snapshot, err := s.options.Repository.Read(ctx)
	if err != nil {
		return result, err
	}
	state, err := s.readRepositoryState(snapshot, true)
	if err != nil {
		return result, err
	}
	ordinaryRecord, ordinaryExists := state.ordinary[input.Name]
	secretRecord, secretExists := state.secret[input.Name]
	if ordinaryExists && secretExists {
		return result, problem(codeState, phaseRead, nil)
	}
	exists := ordinaryExists || secretExists
	if exists && !input.Overwrite {
		return result, invalidInput()
	}
	if input.Secret && ordinaryExists || !input.Secret && secretExists {
		return result, invalidInput()
	}
	machines := append([]string(nil), input.Machines...)
	if len(machines) == 0 {
		switch {
		case input.Secret && len(secretRecord.Machines) != 0:
			machines = append([]string(nil), secretRecord.Machines...)
		case !input.Secret && len(ordinaryRecord.Machines) != 0:
			machines = append([]string(nil), ordinaryRecord.Machines...)
		default:
			machines = []string{"*"}
		}
	}
	entry := record{Value: string(input.Value), Machines: machines}
	if input.Secret {
		state.secret[input.Name] = entry
	} else {
		state.ordinary[input.Name] = entry
	}
	files, cache, err := s.encodeState(state, input.Secret)
	if err != nil {
		return result, err
	}
	if !snapshot.ConfigInitialized {
		files[configManifest] = []byte("schema_version = 1\n")
	}
	progress.step(ctx, "shellenv.commit")
	metadata, err := s.options.Repository.CommitChanges(ctx, snapshot, datarepo.Changes{
		Message: "my: 设置环境变量",
		Files:   files,
	})
	if err != nil {
		return result, err
	}
	progress.confirmed(effectDataCommit)
	progress.commit = metadata.Commit
	progress.step(ctx, phaseCacheWrite)
	cache.Snapshot = metadata
	cache.MachineID = s.options.MachineID
	if err := s.options.Cache.Write(cache); err != nil {
		return result, err
	}
	progress.confirmed(phaseCacheWrite)
	return Result{
		Snapshot: metadata,
		Name:     input.Name,
		Secret:   input.Secret,
		Machines: append([]string(nil), machines...),
		Changed:  true,
	}, nil
}

// Remove 在线删除一个普通或秘密变量。
func (s *Service) Remove(ctx context.Context, input RemoveOptions) (result Result, err error) {
	ctx, progress := s.begin(ctx, "shellenv.remove")
	defer progress.finish(&err)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	progress.step(ctx, phaseRead)

	if err := input.Validate(); err != nil {
		return result, err
	}
	if s.options.Repository == nil || s.options.Cache == nil {
		return result, invalidInput()
	}
	unlock, err := s.options.Cache.Lock(ctx)
	if err != nil {
		return result, err
	}
	defer func() {
		err = errors.Join(err, unlock())
	}()
	snapshot, err := s.options.Repository.Read(ctx)
	if err != nil {
		return result, err
	}
	state, err := s.readRepositoryState(snapshot, true)
	if err != nil {
		return result, err
	}
	var machines []string
	if input.Secret {
		entry, exists := state.secret[input.Name]
		if !exists {
			return result, invalidInput()
		}
		machines = append([]string(nil), entry.Machines...)
		delete(state.secret, input.Name)
	} else {
		entry, exists := state.ordinary[input.Name]
		if !exists {
			return result, invalidInput()
		}
		machines = append([]string(nil), entry.Machines...)
		delete(state.ordinary, input.Name)
	}
	files, cache, err := s.encodeState(state, input.Secret)
	if err != nil {
		return result, err
	}
	progress.step(ctx, "shellenv.commit")
	metadata, err := s.options.Repository.CommitChanges(ctx, snapshot, datarepo.Changes{
		Message: "my: 移除环境变量",
		Files:   files,
	})
	if err != nil {
		return result, err
	}
	progress.confirmed(effectDataCommit)
	progress.commit = metadata.Commit
	progress.step(ctx, phaseCacheWrite)
	cache.Snapshot = metadata
	cache.MachineID = s.options.MachineID
	if err := s.options.Cache.Write(cache); err != nil {
		return result, err
	}
	progress.confirmed(phaseCacheWrite)
	return Result{
		Snapshot: metadata,
		Name:     input.Name,
		Secret:   input.Secret,
		Machines: machines,
		Changed:  true,
	}, nil
}

// List 在线列出变量名称、敏感性和机器范围，不返回值。
func (s *Service) List(ctx context.Context) (result ListResult, err error) {
	ctx, progress := s.begin(ctx, "shellenv.list")
	defer progress.finish(&err)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	progress.step(ctx, phaseRead)

	if s.options.Repository == nil {
		return ListResult{}, invalidInput()
	}
	snapshot, err := s.options.Repository.Read(ctx)
	if err != nil {
		return ListResult{}, err
	}
	state, err := s.readRepositoryState(snapshot, true)
	if err != nil {
		return ListResult{}, err
	}
	result = ListResult{Snapshot: snapshot.Metadata, Variables: []Variable{}}
	for name, entry := range state.ordinary {
		result.Variables = append(result.Variables, Variable{
			Name: name, Machines: append([]string(nil), entry.Machines...),
		})
	}
	for name, entry := range state.secret {
		result.Variables = append(result.Variables, Variable{
			Name: name, Secret: true, Machines: append([]string(nil), entry.Machines...),
		})
	}
	sort.Slice(result.Variables, func(i, j int) bool {
		return result.Variables[i].Name < result.Variables[j].Name
	})
	return result, nil
}

// SyncCache 下载普通声明和秘密密文，不要求 age 身份。
func (s *Service) SyncCache(ctx context.Context) (err error) {
	ctx, progress := s.begin(ctx, "shellenv.synccache")
	defer progress.finish(&err)
	if err := ctx.Err(); err != nil {
		return err
	}
	progress.step(ctx, phaseRead)

	if s.options.Repository == nil || s.options.Cache == nil {
		return invalidInput()
	}
	unlock, err := s.options.Cache.Lock(ctx)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, unlock())
	}()
	snapshot, err := s.options.Repository.Read(ctx)
	if err != nil {
		return err
	}
	state, err := s.readRepositoryState(snapshot, false)
	if err != nil {
		return err
	}
	cache := cacheFromState(snapshot.Metadata, state)
	cache.MachineID = s.options.MachineID
	progress.commit = snapshot.Metadata.Commit
	progress.step(ctx, phaseCacheWrite)
	if err := s.options.Cache.Write(cache); err != nil {
		return err
	}
	progress.confirmed(phaseCacheWrite)
	return nil
}

// Activate 从本机缓存离线渲染指定 shell 的变量赋值。
func (s *Service) Activate(ctx context.Context, shell string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s.options.Cache == nil || s.options.MachineID == "" {
		return "", invalidInput()
	}
	cache, exists, err := s.options.Cache.Read()
	if err != nil {
		return "", err
	}
	if !exists {
		return "", problem(codeState, phaseRead, nil)
	}
	if cache.MachineID != s.options.MachineID {
		return "", problem(codeState, phaseRead, nil)
	}
	if s.options.RepositoryName != "" &&
		(cache.Snapshot.RepositoryID != s.options.RepositoryID ||
			!strings.EqualFold(cache.Snapshot.Repository, s.options.RepositoryName) ||
			cache.Snapshot.Branch != s.options.Branch) {
		return "", problem(codeState, phaseRead, nil)
	}
	ordinary, err := decodeRecords(cache.Ordinary)
	if err != nil {
		return "", err
	}
	secret := map[string]record{}
	if len(cache.SecretCiphertext) != 0 {
		key, err := s.ageKey()
		if err != nil {
			return "", err
		}
		plaintext, err := key.Decrypt(cache.SecretCiphertext, cache.SecretRecipient)
		if err != nil || versionOf(plaintext) != cache.SecretVersion {
			return "", problem(codeState, phaseRead, err)
		}
		secret, err = decodeRecords(plaintext)
		if err != nil {
			return "", err
		}
	}
	for name := range ordinary {
		if _, duplicate := secret[name]; duplicate {
			return "", problem(codeState, phaseRead, nil)
		}
	}
	resolved := []ResolvedVariable{}
	for name, entry := range ordinary {
		if scopeMatches(entry.Machines, s.options.MachineID) {
			resolved = append(resolved, ResolvedVariable{Name: name, Value: entry.Value})
		}
	}
	for name, entry := range secret {
		if scopeMatches(entry.Machines, s.options.MachineID) {
			resolved = append(resolved, ResolvedVariable{Name: name, Value: entry.Value})
		}
	}
	return Render(shell, resolved)
}

func (s *Service) readRepositoryState(snapshot datarepo.Snapshot, decrypt bool) (repositoryState, error) {
	result := repositoryState{
		ordinary: map[string]record{},
		secret:   map[string]record{},
	}
	if !snapshot.Initialized {
		return result, problem(codeState, phaseRead, nil)
	}
	if data, exists := snapshot.Files[ordinaryDataPath()]; exists {
		records, err := decodeRecords(data)
		if err != nil {
			return result, err
		}
		result.ordinary = records
		result.ordinaryData = append([]byte(nil), data...)
	}
	ciphertext, hasSecret := snapshot.Files[secretDataPath()]
	if !hasSecret {
		return result, nil
	}
	metadataData, exists := snapshot.Files[secretMetadataPath()]
	if !exists {
		return result, problem(codeState, phaseRead, nil)
	}
	var metadata targetMetadata
	decoder := toml.NewDecoder(bytes.NewReader(metadataData))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&metadata); err != nil ||
		metadata.SchemaVersion != schemaVersion ||
		metadata.Target != targetSecret ||
		metadata.Version == "" ||
		metadata.Recipient == "" {
		return result, problem(codeState, phaseRead, err)
	}
	result.secretCiphertext = append([]byte(nil), ciphertext...)
	result.secretVersion = metadata.Version
	result.secretRecipient = metadata.Recipient
	if !decrypt {
		return result, nil
	}
	key, err := s.ageKey()
	if err != nil {
		return result, err
	}
	plaintext, err := key.Decrypt(ciphertext, metadata.Recipient)
	if err != nil || versionOf(plaintext) != metadata.Version {
		return result, problem(codeState, phaseRead, err)
	}
	records, err := decodeRecords(plaintext)
	if err != nil {
		return result, err
	}
	result.secret = records
	return result, nil
}

func (s *Service) encodeState(state repositoryState, touchSecret bool) (map[string][]byte, CacheState, error) {
	files := map[string][]byte{}
	ordinaryData, err := encodeRecords(state.ordinary)
	if err != nil {
		return nil, CacheState{}, err
	}
	state.ordinaryData = ordinaryData
	files[ordinaryDataPath()] = ordinaryData
	ordinaryMetadata, err := toml.Marshal(targetMetadata{
		SchemaVersion: schemaVersion,
		Target:        targetOrdinary,
		Machines:      []string{"*"},
	})
	if err != nil {
		return nil, CacheState{}, problem(codeState, phaseWrite, err)
	}
	files[ordinaryMetadataPath()] = ordinaryMetadata

	if touchSecret {
		key, err := s.ageKey()
		if err != nil {
			return nil, CacheState{}, err
		}
		plaintext, err := encodeRecords(state.secret)
		if err != nil {
			return nil, CacheState{}, err
		}
		ciphertext, err := key.Encrypt(plaintext)
		if err != nil {
			return nil, CacheState{}, problem(codeState, phaseWrite, err)
		}
		version := versionOf(plaintext)
		metadata, err := toml.Marshal(targetMetadata{
			SchemaVersion: schemaVersion,
			Target:        targetSecret,
			Machines:      []string{"*"},
			Version:       version,
			Recipient:     key.Recipient(),
		})
		if err != nil {
			return nil, CacheState{}, problem(codeState, phaseWrite, err)
		}
		files[secretMetadataPath()] = metadata
		files[secretDataPath()] = ciphertext
		state.secretCiphertext = ciphertext
		state.secretVersion = version
		state.secretRecipient = key.Recipient()
	}
	return files, cacheFromState(datarepo.Metadata{}, state), nil
}

func cacheFromState(metadata datarepo.Metadata, state repositoryState) CacheState {
	return CacheState{
		Snapshot:         metadata,
		Ordinary:         append([]byte(nil), state.ordinaryData...),
		SecretCiphertext: append([]byte(nil), state.secretCiphertext...),
		SecretVersion:    state.secretVersion,
		SecretRecipient:  state.secretRecipient,
	}
}

func decodeRecords(data []byte) (map[string]record, error) {
	if len(data) == 0 {
		return map[string]record{}, nil
	}
	var document recordsDocument
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil || document.SchemaVersion != schemaVersion {
		return nil, problem(codeState, phaseRead, err)
	}
	if document.Env == nil {
		document.Env = map[string]record{}
	}
	if !validRecords(document.Env) {
		return nil, problem(codeState, phaseRead, nil)
	}

	return document.Env, nil
}

func encodeRecords(records map[string]record) ([]byte, error) {
	if !validRecords(records) {
		return nil, invalidInput()
	}
	return toml.Marshal(recordsDocument{SchemaVersion: schemaVersion, Env: records})
}

func versionOf(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func validName(name string) bool {
	if name == "" || name[0] >= '0' && name[0] <= '9' {
		return false
	}
	for _, r := range name {
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			continue
		}
		return false
	}
	return true
}

type machineScope struct {
	IDs []string
}

func (s machineScope) validate(known []string) error {
	seen := map[string]struct{}{}
	for _, id := range s.IDs {
		if err := machine.ValidateID(id); err != nil {
			return invalidInput()
		}
		if _, exists := seen[id]; exists || !contains(known, id) {
			return invalidInput()
		}
		seen[id] = struct{}{}
	}
	return nil
}

func scopeMatches(machines []string, machineID string) bool {
	if len(machines) == 0 {
		return true
	}
	if len(machines) == 1 && machines[0] == "*" {
		return true
	}
	return contains(machines, machineID)
}

func contains(values []string, want string) bool {
	return slices.Contains(values, want)
}

func ordinaryDataPath() string {
	return path.Join(targetsRoot, targetOrdinary, dataDirectory, envFileName)
}

func ordinaryMetadataPath() string {
	return path.Join(targetsRoot, targetOrdinary, targetMetadataFile)
}

func secretDataPath() string {
	return path.Join(targetsRoot, targetSecret, dataDirectory, envCipherName)
}

func secretMetadataPath() string {
	return path.Join(targetsRoot, targetSecret, targetMetadataFile)
}

func dangerousName(name string) bool {
	if _, exists := dangerousExact[name]; exists {
		return true
	}
	for _, prefix := range dangerousPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

var dangerousExact = map[string]struct{}{
	"PATH": {}, "HOME": {}, "USERPROFILE": {}, "HOMEDRIVE": {}, "HOMEPATH": {},
	"SHELL": {}, "ENV": {}, "BASH_ENV": {}, "BASHOPTS": {}, "BASH_ARGV0": {},
	"BASH_XTRACEFD": {}, "CDPATH": {}, "GLOBIGNORE": {}, "IFS": {},
	"PS1": {}, "PS2": {}, "PS3": {}, "PS4": {}, "PROMPT_COMMAND": {},
	"ZDOTDIR": {}, "FPATH": {}, "COMSPEC": {}, "PATHEXT": {},
	"PSMODULEPATH": {}, "PSEXECUTIONPOLICYPREFERENCE": {},
}

var dangerousPrefixes = []string{
	"LD_", "DYLD_", "BASH_FUNC_", "PYTHON", "NODE_", "RUBYOPT", "RUBYLIB",
	"PERL5OPT", "PERL5LIB", "JAVA_TOOL_OPTIONS", "_JAVA_OPTIONS",
	"JDK_JAVA_OPTIONS", "CLASSPATH", "MISE_", "RTK_",
}

func validRecords(records map[string]record) bool {
	if len(records) > maxRecords {
		return false
	}
	for name, entry := range records {
		if !validName(name) || dangerousName(name) || !validValue([]byte(entry.Value)) {
			return false
		}
		seen := map[string]bool{}
		for _, id := range entry.Machines {
			if seen[id] || id == "*" && len(entry.Machines) != 1 || id != "*" && machine.ValidateID(id) != nil {
				return false
			}
			seen[id] = true
		}
	}
	return true
}
