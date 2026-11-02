package cli

import (
	"testing"

	"golang.org/x/sys/windows"
)

// openWithoutDelete 以 GENERIC_READ + FILE_SHARE_READ|WRITE（**无 DELETE**）
// 打开文件，模拟外部工具占用：Windows 上 os.Remove 将失败（Access denied）。
func openWithoutDelete(t *testing.T, path string) windows.Handle {
	t.Helper()
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatalf("打开句柄失败（探针前提）: %v", err)
	}
	return h
}

func closeHandle(t *testing.T, h windows.Handle) {
	t.Helper()
	if err := windows.CloseHandle(h); err != nil {
		t.Fatalf("关闭句柄失败: %v", err)
	}
}

func loadManifestForTest(t *testing.T, root string) *manifest {
	t.Helper()
	m, err := loadManifest(manifestPath(root))
	if err != nil {
		t.Fatal(err)
	}
	return m
}
