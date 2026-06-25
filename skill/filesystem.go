package skill

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var (
	// ErrLoaderRootRequired is returned when a filesystem loader has no root directory.
	ErrLoaderRootRequired = errors.New("skill: loader root is required")
	// ErrManifestInvalid is returned when a skill manifest cannot be parsed or validated.
	ErrManifestInvalid = errors.New("skill: manifest invalid")
)

// FilesystemLoader loads skill descriptors from an explicit local root.
type FilesystemLoader struct {
	root string
}

// NewFilesystemLoader creates a loader for Agent Skills-style directories.
func NewFilesystemLoader(root string) (*FilesystemLoader, error) {
	if root == "" {
		return nil, ErrLoaderRootRequired
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("skill: resolve loader root: %w", err)
	}
	abs = filepath.Clean(abs)
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("skill: stat loader root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%w: %s is not a directory", ErrLoaderRootRequired, root)
	}
	return &FilesystemLoader{root: abs}, nil
}

// Load scans the root for skill directories and returns lightweight skill descriptors.
func (l *FilesystemLoader) Load(ctx context.Context) ([]Skill, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dirs, err := l.skillDirs(ctx)
	if err != nil {
		return nil, err
	}
	skills := make([]Skill, 0, len(dirs))
	for _, dir := range dirs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		skill, err := l.loadSkill(ctx, dir)
		if err != nil {
			return nil, err
		}
		skills = append(skills, skill)
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills, nil
}

// Register loads skills and registers them into registry.
func (l *FilesystemLoader) Register(ctx context.Context, registry *Registry) error {
	if registry == nil {
		return ErrRegistryRequired
	}
	skills, err := l.Load(ctx)
	if err != nil {
		return err
	}
	for _, skill := range skills {
		if err := registry.Register(ctx, skill); err != nil {
			return err
		}
	}
	return nil
}

func (l *FilesystemLoader) skillDirs(ctx context.Context) ([]string, error) {
	if hasManifest(l.root) {
		return []string{l.root}, nil
	}
	entries, err := os.ReadDir(l.root)
	if err != nil {
		return nil, fmt.Errorf("skill: read loader root: %w", err)
	}
	dirs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(l.root, entry.Name())
		if hasManifest(dir) {
			dirs = append(dirs, dir)
		}
	}
	sort.Strings(dirs)
	return dirs, nil
}

func hasManifest(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "SKILL.md"))
	return err == nil && !info.IsDir()
}

func (l *FilesystemLoader) loadSkill(ctx context.Context, dir string) (Skill, error) {
	manifestPath := filepath.Join(dir, "SKILL.md")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return Skill{}, fmt.Errorf("skill: read manifest: %w", err)
	}
	frontmatter, err := parseFrontmatter(string(raw))
	if err != nil {
		return Skill{}, err
	}
	name := strings.TrimSpace(stringValue(frontmatter["name"]))
	description := strings.TrimSpace(stringValue(frontmatter["description"]))
	if name == "" || description == "" {
		return Skill{}, fmt.Errorf("%w: name and description are required in %s", ErrManifestInvalid, manifestPath)
	}
	resources, err := l.loadResources(ctx, dir)
	if err != nil {
		return Skill{}, err
	}
	scripts, err := l.loadScripts(ctx, dir)
	if err != nil {
		return Skill{}, err
	}
	return Skill{
		Name:        name,
		Description: description,
		Version:     strings.TrimSpace(stringValue(frontmatter["version"])),
		Resources:   resources,
		Scripts:     scripts,
		Metadata:    manifestMetadata(frontmatter),
	}, nil
}

func (l *FilesystemLoader) loadResources(ctx context.Context, dir string) ([]Resource, error) {
	resources := []Resource{{
		Name:     "instructions",
		URI:      l.mustRelSlash(filepath.Join(dir, "SKILL.md")),
		MIMEType: "text/markdown",
	}}
	for _, subdir := range []string{"assets", "references"} {
		root := filepath.Join(dir, subdir)
		info, err := os.Stat(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("skill: stat %s: %w", subdir, err)
		}
		if !info.IsDir() {
			continue
		}
		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			resources = append(resources, Resource{
				Name:     entry.Name(),
				URI:      l.mustRelSlash(path),
				MIMEType: resourceMIMEType(path),
			})
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("skill: walk %s: %w", subdir, err)
		}
	}
	sort.Slice(resources, func(i, j int) bool { return resources[i].URI < resources[j].URI })
	return resources, nil
}

func (l *FilesystemLoader) loadScripts(ctx context.Context, dir string) ([]Script, error) {
	root := filepath.Join(dir, "scripts")
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("skill: stat scripts: %w", err)
	}
	if !info.IsDir() {
		return nil, nil
	}
	var scripts []Script
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		scripts = append(scripts, Script{
			Name:    entry.Name(),
			Command: []string{l.mustRelSlash(path)},
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("skill: walk scripts: %w", err)
	}
	sort.Slice(scripts, func(i, j int) bool { return scripts[i].Name < scripts[j].Name })
	return scripts, nil
}

func (l *FilesystemLoader) mustRelSlash(path string) string {
	rel, err := filepath.Rel(l.root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

type manifestValue struct {
	scalar string
	list   []string
}

func parseFrontmatter(content string) (map[string]manifestValue, error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, fmt.Errorf("%w: missing frontmatter", ErrManifestInvalid)
	}
	values := make(map[string]manifestValue)
	var currentList string
	for i := 1; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			return values, nil
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "- ") && currentList != "" {
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
			entry := values[currentList]
			entry.list = append(entry.list, unquote(value))
			values[currentList] = entry
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("%w: invalid frontmatter line %q", ErrManifestInvalid, line)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			return nil, fmt.Errorf("%w: empty frontmatter key", ErrManifestInvalid)
		}
		if value == "" {
			values[key] = manifestValue{}
			currentList = key
			continue
		}
		values[key] = parseManifestValue(value)
		currentList = ""
	}
	return nil, fmt.Errorf("%w: unterminated frontmatter", ErrManifestInvalid)
}

func parseManifestValue(value string) manifestValue {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"))
		if inner == "" {
			return manifestValue{}
		}
		parts := strings.Split(inner, ",")
		list := make([]string, 0, len(parts))
		for _, part := range parts {
			list = append(list, unquote(strings.TrimSpace(part)))
		}
		return manifestValue{list: list}
	}
	return manifestValue{scalar: unquote(value)}
}

func unquote(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') ||
			(value[0] == '\'' && value[len(value)-1] == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func stringValue(value manifestValue) string {
	return value.scalar
}

func manifestMetadata(values map[string]manifestValue) map[string]any {
	metadata := make(map[string]any)
	for key, value := range values {
		switch key {
		case "name", "description", "version":
			continue
		}
		if len(value.list) > 0 {
			metadata[key] = append([]string(nil), value.list...)
			continue
		}
		if value.scalar != "" {
			metadata[key] = value.scalar
		}
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func resourceMIMEType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown":
		return "text/markdown"
	case ".json":
		return "application/json"
	case ".txt":
		return "text/plain"
	}
	if mimeType := mime.TypeByExtension(filepath.Ext(path)); mimeType != "" {
		if value, _, ok := strings.Cut(mimeType, ";"); ok {
			return strings.TrimSpace(value)
		}
		return mimeType
	}
	return "application/octet-stream"
}
