package main

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/vivaui/viva/tools/manifest"
	"github.com/vivaui/viva/tools/winpkg"
)

// buildWindowsSetup feeds the same payload as the portable ZIP to Viva's NSIS
// packager. This keeps the GoIM build metadata, capture helper and licenses in
// all formats while using the same installer and shortcuts as viva package.
func buildWindowsSetup(staging, setup, version, arch string) error {
	config, err := manifest.Load(".")
	if err != nil {
		return err
	}
	staging, err = filepath.Abs(staging)
	if err != nil {
		return err
	}
	setup, err = filepath.Abs(setup)
	if err != nil {
		return err
	}
	const exe, ico = "GoIM.exe", "GoIM.ico"
	var files []winpkg.ExtraFile
	if err := filepath.WalkDir(staging, func(path string, file fs.DirEntry, err error) error {
		if err != nil || file.IsDir() {
			return err
		}
		relative, err := filepath.Rel(staging, path)
		if err != nil {
			return err
		}
		if relative != exe && relative != ico {
			files = append(files, winpkg.ExtraFile{Source: path, Destination: filepath.ToSlash(relative)})
		}
		return nil
	}); err != nil {
		return err
	}
	if err := winpkg.CheckExtraFiles(files); err != nil {
		return err
	}

	scratch, err := os.MkdirTemp(filepath.Dir(setup), ".goim-setup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	output := filepath.Join(scratch, filepath.Base(setup))
	script := winpkg.Script{
		Name: config.App.Name, Version: version, Publisher: config.Windows.Publisher,
		URL: config.Windows.URL, ID: config.App.ID, Scope: config.Windows.InstallScope(),
		Executable: exe, Program: filepath.Join(staging, exe), Icon: filepath.Join(staging, ico),
		Arch: arch, Files: files, Output: output,
	}
	source, err := script.Generate()
	if err != nil {
		return err
	}
	source, err = requireClosedGoIM(source)
	if err != nil {
		return err
	}
	compiler, err := winpkg.Find()
	if err != nil {
		return err
	}
	filename := filepath.Join(scratch, "installer.nsi")
	if err := os.WriteFile(filename, source, 0o600); err != nil {
		return err
	}
	command := exec.Command(compiler, "-INPUTCHARSET", "UTF8", "-NOCD", "-V2", filename)
	command.Dir = scratch
	command.Env = append(os.Environ(), "SOURCE_DATE_EPOCH=315532800")
	log, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build GoIM Setup: %w\n%s", err, log)
	}
	if len(log) != 0 {
		fmt.Fprint(os.Stderr, string(log))
	}
	return os.Rename(output, setup)
}

func requireClosedGoIM(source []byte) ([]byte, error) {
	text := string(source)
	const start = "Function ${un}EnsureClosed\n"
	begin := strings.Index(text, start)
	if begin < 0 || strings.Count(text, start) != 1 {
		return nil, fmt.Errorf("GoIM Setup: expected one close-check function")
	}
	end := strings.Index(text[begin:], "FunctionEnd\n")
	if end < 0 {
		return nil, fmt.Errorf("GoIM Setup: close-check function is incomplete")
	}
	const guard = `Function ${un}EnsureClosed
  retry:
  FindWindow $R0 "VivaNativeWindow" "GoIM"
  StrCmp $R0 0 check_file
  MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "GoIM 正在运行，请先从系统托盘退出 GoIM，再点击重试。$\r$\nPlease exit GoIM, including its tray icon, before continuing." /SD IDCANCEL IDRETRY retry
  SetErrorLevel 1618
  Abort
  check_file:
  ${IfNot} ${FileExists} "$INSTDIR\${APP_EXE}"
    Return
  ${EndIf}
  ClearErrors
  Delete "$INSTDIR\${APP_EXE}"
  ${If} ${Errors}
    MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "GoIM 文件仍被占用。请退出所有 GoIM 实例后重试。$\r$\nGoIM files are in use. Exit all GoIM instances and retry." /SD IDCANCEL IDRETRY retry
    SetErrorLevel 1618
    Abort
  ${EndIf}
FunctionEnd
`
	return []byte(text[:begin] + guard + text[begin+end+len("FunctionEnd\n"):]), nil
}
