package image

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"covet/internal/store"
)

type Config struct {
	Cmd       []string  `json:"cmd,omitempty"` // 该镜像运行时执行的默认命令
	CreatedAt time.Time `json:"created_at"`
}

type Manifest struct {
	SchemaVersion int      `json:"schema_version"`
	ConfigPath    string   `json:"config_path"`
	Layers        []string `json:"layers"`
}

type Info struct {
	Name         string    `json:"name"`
	Format       string    `json:"format"`
	Path         string    `json:"path"`
	ConfigPath   string    `json:"config_path,omitempty"`
	ManifestPath string    `json:"manifest_path,omitempty"`
	Layers       []string  `json:"layers,omitempty"`
	Cmd          []string  `json:"cmd,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

func Pack(rootfs, imageName string) error {
	if imageName == "" {
		return fmt.Errorf("image name is required")
	}
	if rootfs == "" {
		return fmt.Errorf("rootfs path is required")
	}

	rootfs, err := filepath.Abs(rootfs)
	if err != nil {
		return fmt.Errorf("resolve rootfs path: %w", err)
	}
	info, err := os.Stat(rootfs)
	if err != nil {
		return fmt.Errorf("stat rootfs %q: %w", rootfs, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("rootfs %q must be a directory", rootfs)
	}
	if err := os.MkdirAll(store.ImagesDir(), 0o755); err != nil {
		return fmt.Errorf("create image dir: %w", err)
	}

	imageDir := store.ImageDir(imageName)
	// 创建新镜像之前先删除可能存在的旧镜像
	if err := os.RemoveAll(imageDir); err != nil {
		return fmt.Errorf("reset image dir %q: %w", imageDir, err)
	}
	if err := os.MkdirAll(imageDir, 0o755); err != nil {
		return fmt.Errorf("create image dir %q: %w", imageDir, err)
	}
	if err := os.Remove(store.LegacyImagePath(imageName)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove legacy image tar for %q: %w", imageName, err)
	}

	// 写入 tar 包
	if err := writeLayerTar(rootfs, store.ImageLayerPath(imageName)); err != nil {
		return err
	}
	// 写入 config.json
	if err := writeJSON(store.ImageConfigPath(imageName), Config{
		Cmd:       []string{"/bin/sh"},
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		return err
	}
	// 写入 manifes.json
	if err := writeJSON(store.ImageManifestPath(imageName), Manifest{
		SchemaVersion: 1,
		ConfigPath:    "config.json",
		Layers:        []string{"layer.tar"},
	}); err != nil {
		return err
	}
	return nil
}

func Unpack(imageName, rootfs string) error {
	if imageName == "" {
		return fmt.Errorf("image name is required")
	}
	if rootfs == "" {
		return fmt.Errorf("target rootfs path is required")
	}

	layerPath, err := resolveLayerPath(imageName)
	if err != nil {
		return err
	}

	// 打开 tar 文件
	file, err := os.Open(layerPath)
	if err != nil {
		return fmt.Errorf("open image layer %q: %w", layerPath, err)
	}
	defer file.Close()

	rootfs, err = filepath.Abs(rootfs)
	if err != nil {
		return fmt.Errorf("resolve target rootfs path: %w", err)
	}
	if err := os.MkdirAll(rootfs, 0o755); err != nil {
		return fmt.Errorf("create target rootfs %q: %w", rootfs, err)
	}

	// 准备 reader 从 tar 文件开始读
	reader := tar.NewReader(file)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}

		targetPath := filepath.Join(rootfs, filepath.Clean(header.Name))
		if err := ensureWithinRoot(rootfs, targetPath); err != nil {
			return err
		}

		// 根据每一小段的 header 类型的不同，采取不同的写入策略
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("create dir %q: %w", targetPath, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return fmt.Errorf("create parent dir for %q: %w", targetPath, err)
			}
			out, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("open file %q: %w", targetPath, err)
			}
			if _, err := io.Copy(out, reader); err != nil {
				out.Close()
				return fmt.Errorf("write file %q: %w", targetPath, err)
			}
			if err := out.Close(); err != nil {
				return fmt.Errorf("close file %q: %w", targetPath, err)
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return fmt.Errorf("create parent dir for symlink %q: %w", targetPath, err)
			}
			if err := os.RemoveAll(targetPath); err != nil {
				return fmt.Errorf("remove existing path %q: %w", targetPath, err)
			}
			if err := os.Symlink(header.Linkname, targetPath); err != nil {
				return fmt.Errorf("create symlink %q -> %q: %w", targetPath, header.Linkname, err)
			}
		default:
			return fmt.Errorf("unsupported tar entry type %d for %q", header.Typeflag, header.Name)
		}
	}
}

func Images() ([]string, error) {
	entries, err := os.ReadDir(store.ImagesDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read image dir: %w", err)
	}

	images := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			if _, err := os.Stat(store.ImageManifestPath(entry.Name())); err == nil {
				images[entry.Name()] = struct{}{}
			}
			continue
		}
		if before, ok := strings.CutSuffix(entry.Name(), ".tar"); ok {
			images[before] = struct{}{}
		}
	}

	names := make([]string, 0, len(images))
	for name := range images {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// 一个镜像的默认命令
func DefaultCommand(imageName string) ([]string, error) {
	config, err := ReadConfig(imageName)
	if err != nil {
		return nil, err
	}
	if len(config.Cmd) == 0 {
		return []string{"/bin/sh"}, nil
	}
	return append([]string(nil), config.Cmd...), nil
}

func ReadConfig(imageName string) (Config, error) {
	if imageName == "" {
		return Config{}, fmt.Errorf("image name is required")
	}

	configPath := store.ImageConfigPath(imageName)
	data, err := os.ReadFile(configPath)
	if err == nil {
		var cfg Config
		if err := json.Unmarshal(data, &cfg); err != nil {
			return Config{}, fmt.Errorf("unmarshal image config for %q: %w", imageName, err)
		}
		return cfg, nil
	}
	if !os.IsNotExist(err) {
		return Config{}, fmt.Errorf("read image config for %q: %w", imageName, err)
	}

	_, statErr := os.Stat(store.LegacyImagePath(imageName))
	if statErr == nil {
		return Config{Cmd: []string{"/bin/sh"}}, nil
	}
	if os.IsNotExist(statErr) {
		return Config{}, fmt.Errorf("stat image %q: %w", imageName, statErr)
	}
	return Config{}, fmt.Errorf("stat image %q: %w", imageName, statErr)
}

func Validate(imageName string) error {
	if imageName == "" {
		return fmt.Errorf("image name is required")
	}
	if _, err := os.Stat(store.ImageManifestPath(imageName)); err == nil {
		if _, err := os.Stat(store.ImageConfigPath(imageName)); err != nil {
			return fmt.Errorf("stat image config for %q: %w", imageName, err)
		}
		if _, err := os.Stat(store.ImageLayerPath(imageName)); err != nil {
			return fmt.Errorf("stat image layer for %q: %w", imageName, err)
		}
		return nil
	}
	if _, err := os.Stat(store.LegacyImagePath(imageName)); err != nil {
		return fmt.Errorf("stat image %q: %w", imageName, err)
	}
	return nil
}

func Inspect(imageName string) (Info, error) {
	if imageName == "" {
		return Info{}, fmt.Errorf("image name is required")
	}

	if _, err := os.Stat(store.ImageManifestPath(imageName)); err == nil {
		manifest, err := readManifest(imageName)
		if err != nil {
			return Info{}, err
		}
		config, err := ReadConfig(imageName)
		if err != nil {
			return Info{}, err
		}

		layers := make([]string, 0, len(manifest.Layers))
		for _, layer := range manifest.Layers {
			layers = append(layers, filepath.Join(store.ImageDir(imageName), layer))
		}

		imageDir, err := filepath.Abs(store.ImageDir(imageName))
		if err != nil {
			return Info{}, fmt.Errorf("resolve image dir for %q: %w", imageName, err)
		}
		return Info{
			Name:         imageName,
			Format:       "directory",
			Path:         imageDir,
			ConfigPath:   store.ImageConfigPath(imageName),
			ManifestPath: store.ImageManifestPath(imageName),
			Layers:       layers,
			Cmd:          append([]string(nil), config.Cmd...),
			CreatedAt:    config.CreatedAt,
		}, nil
	}

	legacyPath, err := filepath.Abs(store.LegacyImagePath(imageName))
	if err != nil {
		return Info{}, fmt.Errorf("resolve legacy image path for %q: %w", imageName, err)
	}
	if _, err := os.Stat(legacyPath); err != nil {
		return Info{}, fmt.Errorf("stat image %q: %w", imageName, err)
	}
	return Info{
		Name:   imageName,
		Format: "legacy-tar",
		Path:   legacyPath,
		Layers: []string{legacyPath},
		Cmd:    []string{"/bin/sh"},
	}, nil
}

// 打包 rootfs 为 tar 包
func writeLayerTar(rootfs, layerPath string) error {
	file, err := os.OpenFile(layerPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open image layer %q: %w", layerPath, err)
	}
	defer file.Close()

	// file 是实现了 Go 中的 io.Writer 接口的
	// 因此 tar 包可以基于 file 创建一个 writer 往里面写数据
	writer := tar.NewWriter(file)
	defer writer.Close()

	// 遍历 rootfs 目录下的文件，子目录，软链接等，对每一个项执行 WalkFunc
	return filepath.Walk(rootfs, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == rootfs {
			return nil
		}

		// 为什么要计算相对路径？比如 /var/lib/covet/containers/123/rootfs/etc/hosts 这个 path
		// 如果把这个绝对路径直接塞进 tar 包里，解压时就会创建出相同的深层目录
		// 所以我们基于 rootfs 算出来相对路径 etc/hosts，这样才会是真正的容器跟文件系统
		relPath, err := filepath.Rel(rootfs, path)
		if err != nil {
			return fmt.Errorf("compute relative path for %q: %w", path, err)
		}
		relPath = filepath.ToSlash(relPath)

		// Tar 包的结构是 [Header(记录文件名、权限、大小)] + [Body(文件内容)] 交替出现的
		// 这里把文件的系统元信息（权限 755/644、修改时间等）转换成 tar 需要的 Header
		// 并把名字改成我们刚才算好的相对路径
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("build tar header for %q: %w", path, err)
		}
		header.Name = relPath

		// 处理软链接
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("read symlink %q: %w", path, err)
			}
			header.Linkname = linkTarget
		}

		// 先写入 header
		if err := writer.WriteHeader(header); err != nil {
			return fmt.Errorf("write tar header for %q: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open file %q: %w", path, err)
		}
		defer file.Close()

		// 再 copy content
		if _, err := io.Copy(writer, file); err != nil {
			return fmt.Errorf("copy file %q into tar: %w", path, err)
		}
		return nil
	})
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %q: %w", path, err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}
	return nil
}

func readManifest(imageName string) (Manifest, error) {
	data, err := os.ReadFile(store.ImageManifestPath(imageName))
	if err != nil {
		return Manifest{}, fmt.Errorf("read image manifest for %q: %w", imageName, err)
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("unmarshal image manifest for %q: %w", imageName, err)
	}
	return manifest, nil
}

func resolveLayerPath(imageName string) (string, error) {
	if _, err := os.Stat(store.ImageLayerPath(imageName)); err == nil {
		return store.ImageLayerPath(imageName), nil
	}
	if _, err := os.Stat(store.LegacyImagePath(imageName)); err == nil {
		return store.LegacyImagePath(imageName), nil
	}
	return "", fmt.Errorf("stat image %q: %w", imageName, os.ErrNotExist)
}

func ensureWithinRoot(rootfs, target string) error {
	rootfs = filepath.Clean(rootfs)
	target = filepath.Clean(target)
	prefix := rootfs + string(os.PathSeparator)
	if target != rootfs && !strings.HasPrefix(target, prefix) {
		return fmt.Errorf("tar entry escapes rootfs: %q", target)
	}
	return nil
}
