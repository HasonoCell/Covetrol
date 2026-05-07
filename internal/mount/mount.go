package mount

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Mount struct {
	Source      string `json:"source"`
	Target      string `json:"target"`
	Type        string `json:"type,omitempty"`
	ReadOnly    bool   `json:"read_only,omitempty"`
	SourceIsDir bool   `json:"source_is_dir,omitempty"` // 为什么要分 source？目录挂载和文件挂载准备 target 的方式不同
}

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

// raw 里面每条参数类似于 /tmp/data:/data
// 按 /host:/container[:ro] 的格式解析 -v 参数为 Mount 结构体
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

func parse(spec string) (Mount, error) {
	parts := strings.Split(spec, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return Mount{}, fmt.Errorf("invalid bind mount %q: expected /host:/container[:ro]", spec)
	}

	source, err := filepath.Abs(parts[0])
	if err != nil {
		return Mount{}, fmt.Errorf("resolve mount source %q: %w", parts[0], err)
	}
	info, err := os.Stat(source)
	if err != nil {
		return Mount{}, fmt.Errorf("stat mount source %q: %w", source, err)
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

	return Mount{
		Source:      source,
		Target:      target,
		Type:        "bind",
		ReadOnly:    readOnly,
		SourceIsDir: info.IsDir(),
	}, nil
}
