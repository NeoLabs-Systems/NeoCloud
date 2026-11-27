package backups

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Target is the object-store surface needed by encrypted backup snapshots.
// Keys are slash separated relative paths and never begin with a slash.
type Target interface {
	Open(ctx context.Context) error
	Close() error
	Exists(ctx context.Context, key string) (bool, error)
	List(ctx context.Context, prefix string) ([]string, error)
	Put(ctx context.Context, key string, source string) error
	Get(ctx context.Context, key string, destination string) error
	Delete(ctx context.Context, key string) error
	DeletePrefix(ctx context.Context, prefix string) error
}

// NewTarget constructs the supported local, S3-compatible, SFTP, or WebDAV target implementation.
func NewTarget(kind string, config map[string]any) (Target, error) {
	switch kind {
	case "local":
		root, _ := config["path"].(string)
		return NewLocalTarget(root)
	case "webdav":
		urlValue, _ := config["url"].(string)
		username, _ := config["username"].(string)
		password, _ := config["password"].(string)
		return NewWebDAVTarget(WebDAVConfig{URL: urlValue, Username: username, Password: password})
	case "s3":
		return NewS3Target(S3Config{
			Endpoint: stringConfig(config["endpoint"]), Region: stringConfig(config["region"]),
			Bucket: stringConfig(config["bucket"]), AccessKeyID: stringConfig(config["accessKeyId"]),
			SecretAccessKey: stringConfig(config["secretAccessKey"]), Prefix: stringConfig(config["prefix"]),
			ForcePathStyle: boolConfig(config["forcePathStyle"]), StorageClass: stringConfig(config["storageClass"]),
		})
	case "sftp":
		port := 22
		if value, ok := config["port"].(float64); ok && value > 0 {
			port = int(value)
		} else if value, ok := config["port"].(int); ok && value > 0 {
			port = value
		}
		return NewSFTPTarget(SFTPConfig{
			Host: stringConfig(config["host"]), Port: port,
			Username: stringConfig(config["username"]), Password: stringConfig(config["password"]),
			PrivateKey: stringConfig(config["privateKey"]), Passphrase: stringConfig(config["passphrase"]),
			Path: stringConfig(config["path"]), HostKey: stringConfig(config["hostKey"]),
		})
	default:
		return nil, errors.New("backup target type is not supported")
	}
}

// OpenTargetSpec accepts either an existing local target directory or a JSON
// file containing {"type":"s3|sftp|webdav|local","config":{...}}.
func OpenTargetSpec(path string) (Target, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return NewLocalTarget(absolute)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("backup target spec must be a regular file")
	}
	if info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("backup target spec permissions must be private (0600 or stricter)")
	}
	file, err := os.Open(absolute)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var spec struct {
		Type   string         `json:"type"`
		Config map[string]any `json:"config"`
	}
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&spec); err != nil {
		return nil, err
	}
	if spec.Type == "" || spec.Config == nil {
		return nil, errors.New("backup target spec requires type and config")
	}
	return NewTarget(spec.Type, spec.Config)
}

func stringConfig(value any) string {
	text, _ := value.(string)
	return text
}

func boolConfig(value any) bool {
	flag, _ := value.(bool)
	return flag
}

type LocalTarget struct{ root string }

func NewLocalTarget(root string) (*LocalTarget, error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("local backup target path must be absolute")
	}
	return &LocalTarget{root: filepath.Clean(root)}, nil
}

func (t *LocalTarget) path(key string) (string, error) {
	if err := validTargetKey(key, true); err != nil {
		return "", err
	}
	joined := filepath.Join(t.root, filepath.FromSlash(key))
	rel, err := filepath.Rel(t.root, joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("local backup key escapes target")
	}
	return joined, nil
}

func (*LocalTarget) Open(context.Context) error { return nil }
func (*LocalTarget) Close() error               { return nil }

func (t *LocalTarget) Exists(_ context.Context, key string) (bool, error) {
	name, err := t.path(key)
	if err != nil {
		return false, err
	}
	info, err := os.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.Mode().IsRegular(), nil
}

func (t *LocalTarget) List(_ context.Context, prefix string) ([]string, error) {
	base, err := t.path(prefix)
	if err != nil {
		return nil, err
	}
	out := []string{}
	err = filepath.WalkDir(base, func(name string, entry fs.DirEntry, walkErr error) error {
		if errors.Is(walkErr, fs.ErrNotExist) && name == base {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(t.root, name)
		if relErr != nil {
			return relErr
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	return out, err
}

func (t *LocalTarget) Put(ctx context.Context, key, source string) error {
	destination, err := t.path(key)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	return copyDurable(ctx, source, destination)
}

func (t *LocalTarget) Get(ctx context.Context, key, destination string) error {
	source, err := t.path(key)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	return copyDurable(ctx, source, destination)
}

func (t *LocalTarget) Delete(_ context.Context, key string) error {
	name, err := t.path(key)
	if err != nil {
		return err
	}
	err = os.Remove(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func (t *LocalTarget) DeletePrefix(_ context.Context, prefix string) error {
	name, err := t.path(prefix)
	if err != nil {
		return err
	}
	err = os.RemoveAll(name)
	return err
}
