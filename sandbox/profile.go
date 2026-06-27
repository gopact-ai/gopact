package sandbox

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

var (
	// ErrProfileViolation is returned when a sandbox operation exceeds a Profile.
	ErrProfileViolation = errors.New("sandbox: profile violation")
)

// Profile is a fail-closed, host-owned sandbox capability profile.
type Profile struct {
	Name              string
	AllowedCommands   []string
	AllowedReadPaths  []string
	AllowedWritePaths []string
	AllowedEnvKeys    []string
	Limits            ResourceLimits
	Metadata          map[string]any
}

// ApplySpec returns a copy of spec constrained by the profile limits.
func (p Profile) ApplySpec(spec Spec) (Spec, error) {
	out := copySpec(spec)
	if err := p.checkEnv(out.Env); err != nil {
		return Spec{}, err
	}
	if err := p.checkLimit("create", "output bytes", out.Limits.OutputBytes, p.Limits.OutputBytes); err != nil {
		return Spec{}, err
	}
	if err := p.checkLimit("create", "timeout", int64(out.Limits.Timeout), int64(p.Limits.Timeout)); err != nil {
		return Spec{}, err
	}
	if out.Limits.OutputBytes == 0 {
		out.Limits.OutputBytes = p.Limits.OutputBytes
	}
	if out.Limits.Timeout == 0 {
		out.Limits.Timeout = p.Limits.Timeout
	}
	return out, nil
}

// CheckExec verifies req against the profile's command and timeout limits.
func (p Profile) CheckExec(req ExecRequest) error {
	if len(req.Command) == 0 || strings.TrimSpace(req.Command[0]) == "" {
		return profileViolation("exec", "command is required")
	}
	if !profileStringAllowed(filepath.Base(req.Command[0]), p.AllowedCommands) {
		return profileViolation("exec", "command is not allowed")
	}
	if err := p.checkLimit("exec", "timeout", int64(req.Timeout), int64(p.Limits.Timeout)); err != nil {
		return err
	}
	return nil
}

// CheckReadFile verifies path against the profile's read allowlist.
func (p Profile) CheckReadFile(path string) error {
	if !profilePathAllowed(path, p.AllowedReadPaths) {
		return profileViolation("read file", "path is not allowed")
	}
	return nil
}

// CheckWriteFile verifies file against the profile's write allowlist.
func (p Profile) CheckWriteFile(file File) error {
	if !profilePathAllowed(file.Path, p.AllowedWritePaths) {
		return profileViolation("write file", "path is not allowed")
	}
	return nil
}

// ProfileManager applies a Profile before delegating to another Manager.
type ProfileManager struct {
	next    Manager
	profile Profile
}

// NewProfileManager wraps a sandbox manager with profile checks.
func NewProfileManager(next Manager, profile Profile) (*ProfileManager, error) {
	if next == nil {
		return nil, ErrManagerRequired
	}
	return &ProfileManager{
		next:    next,
		profile: copyProfile(profile),
	}, nil
}

// Create validates and constrains spec before creating a profiled session.
func (m *ProfileManager) Create(ctx context.Context, spec Spec) (Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	constrained, err := m.profile.ApplySpec(spec)
	if err != nil {
		return nil, err
	}
	session, err := m.next.Create(ctx, constrained)
	if err != nil {
		return nil, err
	}
	return &profileSession{next: session, profile: m.profile}, nil
}

type profileSession struct {
	next    Session
	profile Profile
}

func (s *profileSession) ID() string {
	return s.next.ID()
}

func (s *profileSession) Exec(ctx context.Context, req ExecRequest) (ExecResult, error) {
	if err := ctx.Err(); err != nil {
		return ExecResult{}, err
	}
	if err := s.profile.CheckExec(req); err != nil {
		return ExecResult{}, err
	}
	return s.next.Exec(ctx, req)
}

func (s *profileSession) ReadFile(ctx context.Context, path string) (File, error) {
	if err := ctx.Err(); err != nil {
		return File{}, err
	}
	if err := s.profile.CheckReadFile(path); err != nil {
		return File{}, err
	}
	return s.next.ReadFile(ctx, path)
}

func (s *profileSession) WriteFile(ctx context.Context, file File) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.profile.CheckWriteFile(file); err != nil {
		return err
	}
	return s.next.WriteFile(ctx, file)
}

func (s *profileSession) Close(ctx context.Context) error {
	return s.next.Close(ctx)
}

func (p Profile) checkEnv(env map[string]string) error {
	for key := range env {
		if !profileStringAllowed(key, p.AllowedEnvKeys) {
			return profileViolation("create", "env key is not allowed")
		}
	}
	return nil
}

func (p Profile) checkLimit(operation string, name string, value int64, limit int64) error {
	if value <= 0 || limit <= 0 {
		return nil
	}
	if value > limit {
		return profileViolation(operation, name+" exceeds profile limit")
	}
	return nil
}

func profileStringAllowed(value string, allowed []string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func profilePathAllowed(path string, allowedRoots []string) bool {
	cleaned, ok := cleanProfilePath(path)
	if !ok {
		return false
	}
	for _, root := range allowedRoots {
		cleanRoot, ok := cleanProfilePath(root)
		if !ok {
			continue
		}
		if cleanRoot == "." || cleaned == cleanRoot || strings.HasPrefix(cleaned, cleanRoot+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func cleanProfilePath(path string) (string, bool) {
	if path == "" || !filepath.IsLocal(path) {
		return "", false
	}
	return filepath.Clean(path), true
}

func copyProfile(profile Profile) Profile {
	profile.AllowedCommands = append([]string(nil), profile.AllowedCommands...)
	profile.AllowedReadPaths = append([]string(nil), profile.AllowedReadPaths...)
	profile.AllowedWritePaths = append([]string(nil), profile.AllowedWritePaths...)
	profile.AllowedEnvKeys = append([]string(nil), profile.AllowedEnvKeys...)
	profile.Metadata = copyAnyMap(profile.Metadata)
	return profile
}

func profileViolation(operation string, reason string) error {
	return fmt.Errorf("%w: %s: %s", ErrProfileViolation, operation, reason)
}
