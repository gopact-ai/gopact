// Package fileblob provides a filesystem-backed object/blob adapter.
package fileblob

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/checkpoint"
)

var (
	ErrRootRequired = errors.New("fileblob: root is required")
	ErrUnsafeKey    = errors.New("fileblob: unsafe key")
)

// Backend stores object/blob payloads under a filesystem root.
type Backend struct {
	root string
}

var _ checkpoint.ObjectBackend = (*Backend)(nil)
var _ gopact.TurnLoopBlobBackend = (*Backend)(nil)

// NewBackend creates a filesystem-backed object/blob backend.
func NewBackend(root string) (*Backend, error) {
	if root == "" {
		return nil, ErrRootRequired
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("fileblob: resolve root: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("fileblob: create root: %w", err)
	}
	return &Backend{root: abs}, nil
}

// PutObject stores or replaces one checkpoint object payload.
func (b *Backend) PutObject(ctx context.Context, key string, data []byte) error {
	return b.put(ctx, key, data)
}

// GetObject returns a copy of one checkpoint object payload.
func (b *Backend) GetObject(ctx context.Context, key string) ([]byte, error) {
	raw, err := b.get(ctx, key)
	if errors.Is(err, os.ErrNotExist) {
		return nil, checkpoint.ErrObjectNotFound
	}
	return raw, err
}

// ListObjects returns objects whose keys are under prefix.
func (b *Backend) ListObjects(ctx context.Context, prefix string) ([]checkpoint.ObjectInfo, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, normalizedPrefix, err := b.resolvePrefix(prefix)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("fileblob: stat prefix: %w", err)
	}

	var infos []checkpoint.ObjectInfo
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		key, err := b.keyFromPath(path)
		if err != nil {
			return err
		}
		if normalizedPrefix != "" && !strings.HasPrefix(key, normalizedPrefix) {
			return nil
		}
		infos = append(infos, checkpoint.ObjectInfo{
			Key:       key,
			UpdatedAt: info.ModTime().UTC(),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("fileblob: list objects: %w", err)
	}
	sort.SliceStable(infos, func(i, j int) bool {
		return infos[i].Key < infos[j].Key
	})
	return infos, nil
}

// PutBlob stores or replaces one TurnLoop blob payload.
func (b *Backend) PutBlob(ctx context.Context, key string, data []byte) error {
	return b.put(ctx, key, data)
}

// GetBlob returns a copy of one TurnLoop blob payload.
func (b *Backend) GetBlob(ctx context.Context, key string) ([]byte, error) {
	raw, err := b.get(ctx, key)
	if errors.Is(err, os.ErrNotExist) {
		return nil, gopact.ErrTurnLoopBlobNotFound
	}
	return raw, err
}

func (b *Backend) put(ctx context.Context, key string, data []byte) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := b.resolveKey(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("fileblob: create object directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("fileblob: create temp object: %w", err)
	}
	tmpName := tmp.Name()
	closed := false
	defer func() {
		if !closed {
			_ = tmp.Close()
		}
		_ = os.Remove(tmpName)
	}()
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("fileblob: write temp object: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("fileblob: close temp object: %w", err)
	}
	closed = true
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("fileblob: replace object: %w", err)
	}
	now := time.Now()
	_ = os.Chtimes(path, now, now)
	return nil
}

func (b *Backend) get(ctx context.Context, key string) ([]byte, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := b.resolveKey(key)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("fileblob: read object: %w", err)
	}
	return raw, nil
}

func (b *Backend) resolveKey(key string) (string, error) {
	parts, err := cleanKeyParts(key, false)
	if err != nil {
		return "", err
	}
	return b.joinSafe(parts...)
}

func (b *Backend) resolvePrefix(prefix string) (string, string, error) {
	parts, err := cleanKeyParts(prefix, true)
	if err != nil {
		return "", "", err
	}
	if len(parts) == 0 {
		return b.root, "", nil
	}
	normalized := strings.Join(parts, "/")
	if strings.HasSuffix(prefix, "/") {
		normalized += "/"
	}
	path, err := b.joinSafe(parts...)
	return path, normalized, err
}

func (b *Backend) joinSafe(parts ...string) (string, error) {
	if b == nil || b.root == "" {
		return "", ErrRootRequired
	}
	path := filepath.Join(append([]string{b.root}, parts...)...)
	rel, err := filepath.Rel(b.root, path)
	if err != nil {
		return "", fmt.Errorf("fileblob: resolve key: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", ErrUnsafeKey
	}
	return path, nil
}

func (b *Backend) keyFromPath(path string) (string, error) {
	rel, err := filepath.Rel(b.root, path)
	if err != nil {
		return "", fmt.Errorf("fileblob: resolve listed object: %w", err)
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", ErrUnsafeKey
	}
	return filepath.ToSlash(rel), nil
}

func cleanKeyParts(key string, allowEmpty bool) ([]string, error) {
	if key == "" {
		if allowEmpty {
			return nil, nil
		}
		return nil, ErrUnsafeKey
	}
	if filepath.IsAbs(key) || strings.HasPrefix(key, "/") || strings.Contains(key, "\\") {
		return nil, ErrUnsafeKey
	}
	key = strings.TrimSuffix(key, "/")
	if key == "" {
		if allowEmpty {
			return nil, nil
		}
		return nil, ErrUnsafeKey
	}
	parts := strings.Split(key, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return nil, ErrUnsafeKey
		}
	}
	return parts, nil
}
