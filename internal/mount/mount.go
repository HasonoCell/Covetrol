package mount

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Mount struct {
	Source      string `json:"source"` // 要求宿主机视角下的绝对路径
	Target      string `json:"target"` // 要求宿主机视角下的绝对路径
	Name        string `json:"name,omitempty"`
	Type        string `json:"type,omitempty"`
	ReadOnly    bool   `json:"read_only,omitempty"`
	SourceIsDir bool   `json:"source_is_dir,omitempty"` // 为什么要分 source？目录挂载和文件挂载准备 target 的方式不同
}

type VolumeInfo struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
}

// 分为绑定挂载和挂载卷
//	-v /host:/container[:ro]：bind mount
//	-v mydata:/container[:ro]：named volume

const (
	TypeBind   = "bind"
	TypeVolume = "volume"
)

var volumeNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

type List []string

// String 和 Set 主要是 flag.Value 接口要求实现的方法，用来收集命令参数
func (m *List) String() string {
	return strings.Join(*m, ",")
}

func (m *List) Set(value string) error {
	if value == "" {
		return fmt.Errorf("mount specification cannot be empty")
	}
	*m = append(*m, value)
	return nil
}

// 解析 -v 参数为 Mount 结构体
func Parse(raw []string) ([]Mount, error) {
	mounts := make([]Mount, 0, len(raw))
	for _, spec := range raw {
		mount, err := parse(spec)
		if err != nil {
			return nil, err
		}
		mounts = append(mounts, mount)
	}
	return mounts, nil
}

// 实际的解析函数，区分 bind mount 和 volume
func parse(spec string) (Mount, error) {
	parts := strings.Split(spec, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return Mount{}, fmt.Errorf("invalid mount %q: expected /host:/container[:ro] or volume:/container[:ro]", spec)
	}

	target := filepath.Clean(parts[1])
	if !filepath.IsAbs(target) {
		return Mount{}, fmt.Errorf("mount target %q must be an absolute container path", parts[1])
	}

	readOnly := false
	if len(parts) == 3 {
		switch parts[2] {
		case "ro":
			readOnly = true
		case "rw", "":
		default:
			return Mount{}, fmt.Errorf("invalid mount mode %q in %q: only ro or rw are supported", parts[2], spec)
		}
	}

	if filepath.IsAbs(parts[0]) {
		return parseBindMount(parts[0], target, readOnly)
	}
	return parseVolumeMount(parts[0], target, readOnly)
}

// 解析绑定挂载
func parseBindMount(sourceSpec, target string, readOnly bool) (Mount, error) {
	source, err := filepath.Abs(sourceSpec)
	if err != nil {
		return Mount{}, fmt.Errorf("resolve mount source %q: %w", sourceSpec, err)
	}
	info, err := os.Stat(source)
	if err != nil {
		return Mount{}, fmt.Errorf("stat mount source %q: %w", source, err)
	}

	return Mount{
		Source:      source,
		Target:      target,
		Type:        TypeBind,
		ReadOnly:    readOnly,
		SourceIsDir: info.IsDir(),
	}, nil
}

// 解析卷挂载
func parseVolumeMount(name, target string, readOnly bool) (Mount, error) {
	if !volumeNamePattern.MatchString(name) {
		return Mount{}, fmt.Errorf("invalid volume name %q: only letters, numbers, dot, underscore, and dash are supported", name)
	}

	source, err := ensureVolumeDir(name)
	if err != nil {
		return Mount{}, err
	}

	return Mount{
		Source:      source,
		Target:      target,
		Name:        name,
		Type:        TypeVolume,
		ReadOnly:    readOnly,
		SourceIsDir: true,
	}, nil
}

func ensureVolumeDir(name string) (string, error) {
	source := filepath.Join(volumesBaseDir(), name)
	absSource, err := filepath.Abs(source)
	if err != nil {
		return "", fmt.Errorf("resolve volume path for %q: %w", name, err)
	}
	if err := os.MkdirAll(absSource, 0o755); err != nil {
		return "", fmt.Errorf("create volume %q at %q: %w", name, absSource, err)
	}
	return absSource, nil
}

func volumesBaseDir() string {
	return filepath.Join(".covet", "volumes")
}

// 解析 volume 存储路径
func VolumePath(name string) string {
	return filepath.Join(volumesBaseDir(), name)
}

func ListVolumes() ([]string, error) {
	entries, err := os.ReadDir(volumesBaseDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read volumes dir: %w", err)
	}

	volumes := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if !volumeNamePattern.MatchString(entry.Name()) {
			continue
		}
		volumes = append(volumes, entry.Name())
	}
	sort.Strings(volumes)
	return volumes, nil
}

func RemoveVolume(name string) error {
	if !volumeNamePattern.MatchString(name) {
		return fmt.Errorf("invalid volume name %q: only letters, numbers, dot, underscore, and dash are supported", name)
	}

	path := VolumePath(name)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("volume %q does not exist", name)
		}
		return fmt.Errorf("stat volume %q: %w", name, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("volume %q path %q is not a directory", name, path)
	}

	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove volume %q: %w", name, err)
	}
	return nil
}

// 排查 volume 信息
func InspectVolume(name string) (VolumeInfo, error) {
	if !volumeNamePattern.MatchString(name) {
		return VolumeInfo{}, fmt.Errorf("invalid volume name %q: only letters, numbers, dot, underscore, and dash are supported", name)
	}

	path := VolumePath(name)
	absPath, err := filepath.Abs(path)
	if err != nil {
		return VolumeInfo{}, fmt.Errorf("resolve volume path for %q: %w", name, err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return VolumeInfo{
				Name:   name,
				Path:   absPath,
				Exists: false,
			}, nil
		}
		return VolumeInfo{}, fmt.Errorf("stat volume %q: %w", name, err)
	}
	if !info.IsDir() {
		return VolumeInfo{}, fmt.Errorf("volume %q path %q is not a directory", name, absPath)
	}

	return VolumeInfo{
		Name:   name,
		Path:   absPath,
		Exists: true,
	}, nil
}
