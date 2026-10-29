package quota

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func FindCodex(override string) (string, error) {
	if override == "" {
		override = os.Getenv("CODEX_QUOTA_CODEX_PATH")
	}
	if override != "" {
		if nativeFile(override) {
			return override, nil
		}
		return "", Fail("not_installed")
	}
	name := "codex"
	if runtime.GOOS == "windows" {
		name = "codex.exe"
	}
	if p, e := exec.LookPath(name); e == nil {
		resolved, _ := filepath.EvalSymlinks(p)
		if strings.HasSuffix(resolved, ".js") {
			if n := npmNative(filepath.Dir(filepath.Dir(resolved))); n != "" {
				return n, nil
			}
		} else {
			return p, nil
		}
	}
	if runtime.GOOS == "windows" {
		roots := []string{filepath.Join(os.Getenv("APPDATA"), "npm", "node_modules", "@openai", "codex")}
		for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
			roots = append(roots, filepath.Join(dir, "node_modules", "@openai", "codex"))
		}
		for _, root := range roots {
			if n := npmNative(root); n != "" {
				return n, nil
			}
		}
	}
	return "", Fail("not_installed")
}
func nativeFile(p string) bool {
	st, e := os.Stat(p)
	return e == nil && !st.IsDir() && (runtime.GOOS != "windows" || strings.EqualFold(filepath.Ext(p), ".exe"))
}
func npmNative(root string) string {
	arch := "x86_64"
	pkgArch := "x64"
	if runtime.GOARCH == "arm64" {
		arch = "aarch64"
		pkgArch = "arm64"
	}
	triple, exe := arch+"-unknown-linux-musl", "codex"
	if runtime.GOOS == "darwin" {
		triple = arch + "-apple-darwin"
	}
	if runtime.GOOS == "windows" {
		triple = arch + "-pc-windows-msvc"
		exe = "codex.exe"
	}
	pkgOS := runtime.GOOS
	if pkgOS == "windows" {
		pkgOS = "win32"
	}
	tail := filepath.Join("vendor", triple, "bin", exe)
	candidates := []string{filepath.Join(root, tail), filepath.Join(root, "node_modules", "@openai", "codex-"+pkgOS+"-"+pkgArch, tail), filepath.Join(filepath.Dir(root), "codex-"+pkgOS+"-"+pkgArch, tail)}
	for _, p := range candidates {
		if nativeFile(p) {
			return p
		}
	}
	return ""
}
