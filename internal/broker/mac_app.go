package broker

import (
	"archive/zip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const macAppInfo = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleDevelopmentRegion</key><string>zh_CN</string>
<key>CFBundleDisplayName</key><string>Assist</string>
<key>CFBundleExecutable</key><string>launcher</string>
<key>CFBundleIdentifier</key><string>dev.assistdemo.temporary.INVITE_ID</string>
<key>CFBundleInfoDictionaryVersion</key><string>6.0</string>
<key>CFBundleName</key><string>Assist</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleShortVersionString</key><string>0.2.0</string>
<key>CFBundleVersion</key><string>1</string>
<key>LSMinimumSystemVersion</key><string>11.0</string>
</dict></plist>
`

const macAppLauncher = `#!/bin/sh
set -eu
app_dir=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
exec /usr/bin/open -a Terminal "$app_dir/Contents/Resources/start.command"
`

const macAppStart = `#!/bin/sh
app_dir=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd) || exit 1
ticket=$(sed -n '1p' "$app_dir/Contents/Resources/invite.txt")
case "$ticket" in
  ''|*[!A-Za-z0-9_-]*) printf '邀请信息无效，请重新下载 Assist App。\n'; exit 1 ;;
esac
case "$(uname -m)" in
  arm64) binary=assist-arm64 ;;
  x86_64) binary=assist-amd64 ;;
  *) printf '此 Mac 架构暂不支持。\n'; exit 1 ;;
esac
"$app_dir/Contents/MacOS/$binary" "assist://join/$ticket"
status=$?
if [ "$status" -ne 0 ]; then
  printf '\n连接失败（退出码 %s）。请查看上面的原因，按回车关闭窗口。\n' "$status"
  read -r unused
fi
exit "$status"
`

func addZipBytes(z *zip.Writer, name string, mode os.FileMode, data []byte) error {
	h := &zip.FileHeader{Name: name, Method: zip.Deflate}
	h.SetMode(mode)
	h.SetModTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC))
	w, err := z.CreateHeader(h)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func addZipFile(z *zip.Writer, name, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := &zip.FileHeader{Name: name, Method: zip.Deflate}
	h.SetMode(0755)
	h.SetModTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC))
	w, err := z.CreateHeader(h)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}

// writeMacApp packages both native architectures in one request-bound App.
// The invitation is inside the bundle, so Finder renaming the ZIP is harmless.
func writeMacApp(out io.Writer, assets, invitation string) error {
	for _, binary := range []string{"assist-darwin-arm64", "assist-darwin-amd64"} {
		if st, err := os.Stat(filepath.Join(assets, binary)); err != nil || !st.Mode().IsRegular() {
			return fmt.Errorf("缺少 Mac 原生程序 %s；请先运行构建脚本", binary)
		}
	}
	z := zip.NewWriter(out)
	identity := sha256.Sum256([]byte(invitation))
	info := strings.Replace(macAppInfo, "INVITE_ID", fmt.Sprintf("%x", identity[:8]), 1)
	entries := []struct {
		name string
		mode os.FileMode
		data string
	}{
		{"Assist.app/Contents/Info.plist", 0644, info},
		{"Assist.app/Contents/MacOS/launcher", 0755, macAppLauncher},
		{"Assist.app/Contents/Resources/start.command", 0755, macAppStart},
		{"Assist.app/Contents/Resources/invite.txt", 0600, invitation + "\n"},
	}
	for _, entry := range entries {
		if err := addZipBytes(z, entry.name, entry.mode, []byte(entry.data)); err != nil {
			z.Close()
			return err
		}
	}
	for _, binary := range []struct{ source, target string }{
		{"assist-darwin-arm64", "Assist.app/Contents/MacOS/assist-arm64"},
		{"assist-darwin-amd64", "Assist.app/Contents/MacOS/assist-amd64"},
	} {
		if err := addZipFile(z, binary.target, filepath.Join(assets, binary.source)); err != nil {
			z.Close()
			return err
		}
	}
	return z.Close()
}
