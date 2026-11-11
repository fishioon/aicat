//go:build !nogui && !e2e

package app

import "github.com/egoist/mygo"

func chooseTrafficExport(w *mygo.Window) (string, error) {
	return mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: w, Title: tr("导出记录"), DefaultPath: "aicat-traffic.json", Filters: []mygo.FileFilter{{Name: "JSON", Extensions: []string{"json"}}}})
}
