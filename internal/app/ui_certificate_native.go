//go:build !nogui && !e2e

package app

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"

	"github.com/egoist/mygo"
	"github.com/fishioon/aicat/internal/app/locales"
)

func chooseCertificateExport(w *mygo.Window) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	directory := filepath.Join(home, "Downloads")
	if info, e := os.Stat(directory); e != nil || !info.IsDir() {
		directory = home
	}
	return mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: w, Title: locales.ExportRootCertificate(), DefaultPath: filepath.Join(directory, "aicat-root-ca.pem"), ButtonLabel: locales.Export(), Filters: []mygo.FileFilter{{Name: locales.PublicCertificate(), Extensions: []string{"pem"}}}})
}
func openCertificateKeychain() error {
	if goruntime.GOOS != "darwin" {
		return errors.New("当前平台不支持 macOS 钥匙串")
	}
	return exec.Command("/usr/bin/open", "-b", "com.apple.keychainaccess").Run()
}
