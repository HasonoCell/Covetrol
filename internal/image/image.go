package image

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"covet/internal/store"
)

func Commit(rootfs, imageName string) error {
	if imageName == "" {
		return fmt.Errorf("image name is required")
	}
	if rootfs == "" {
		return fmt.Errorf("rootfs path is required")
	}

	// 获取 rootfs 的绝对路径
	rootfs, err := filepath.Abs(rootfs)
	if err != nil {
		return fmt.Errorf("resolve rootfs path: %w", err)
	}

	// 获取一下 rootfs 的文件 info
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

	// 根据 imageName 拼接出 image path
	imagePath := store.ImagePath(imageName)
	// 打开文件，不存在就创建
	file, err := os.OpenFile(imagePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open image tar %q: %w", imagePath, err)
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
		relPath = filepath.ToSlash(relPath) // 防止反斜杠

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

		// 再写入 content
		if _, err := io.Copy(writer, file); err != nil {
			return fmt.Errorf("copy file %q into tar: %w", path, err)
		}
		return nil
	})
}

func Import(imageName, rootfs string) error {
	if imageName == "" {
		return fmt.Errorf("image name is required")
	}
	if rootfs == "" {
		return fmt.Errorf("target rootfs path is required")
	}

	imagePath := store.ImagePath(imageName)
	// 打开镜像压缩包
	file, err := os.Open(imagePath)
	if err != nil {
		return fmt.Errorf("open image tar %q: %w", imagePath, err)
	}
	defer file.Close()

	rootfs, err = filepath.Abs(rootfs)
	if err != nil {
		return fmt.Errorf("resolve target rootfs path: %w", err)
	}
	if err := os.MkdirAll(rootfs, 0o755); err != nil {
		return fmt.Errorf("create target rootfs %q: %w", rootfs, err)
	}

	// 创建个 reader 准备从里面开读
	reader := tar.NewReader(file)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}

		// 最终的解压缩路径基于目标容器的 rootfs 加上压缩包原本的文件路径拼接而成
		targetPath := filepath.Join(rootfs, filepath.Clean(header.Name))
		if err := ensureWithinRoot(rootfs, targetPath); err != nil {
			return err
		}

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

func List() ([]string, error) {
	entries, err := os.ReadDir(store.ImagesDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read image dir: %w", err)
	}

	images := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if before, ok := strings.CutSuffix(name, ".tar"); ok {
			images = append(images, before)
		}
	}
	sort.Strings(images)
	return images, nil
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
