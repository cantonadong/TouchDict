//go:build windows

package mainwindow

import (
	"bytes"
	"encoding/csv"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"
)

func (w *Window) addHistoryMenu() error {
	menu, err := walk.NewMenu()
	if err != nil {
		return err
	}
	action := walk.NewMenuAction(menu)
	action.SetText("历史记录")
	if err := w.MW.Menu().Actions().Add(action); err != nil {
		menu.Dispose()
		return err
	}
	clear := walk.NewAction()
	clear.SetText("清空历史记录…")
	clear.Triggered().Attach(w.clearHistory)
	if err := menu.Actions().Add(clear); err != nil {
		return err
	}
	export := walk.NewAction()
	export.SetText("导出查词记录…")
	export.Triggered().Attach(w.exportHistory)
	return menu.Actions().Add(export)
}

func (w *Window) exportHistory() {
	dialog := walk.FileDialog{
		Title:    "导出查词记录",
		Filter:   "CSV 文件 (*.csv)|*.csv",
		Flags:    win.OFN_OVERWRITEPROMPT,
		FilePath: "查词记录-" + time.Now().Format("2006-01-02") + ".csv",
	}
	ok, err := dialog.ShowSave(w.MW)
	if err != nil {
		walk.MsgBox(w.MW, "TouchDict", "无法选择保存位置："+err.Error(), walk.MsgBoxIconError)
		return
	}
	if !ok {
		return
	}
	if filepath.Ext(dialog.FilePath) == "" {
		dialog.FilePath += ".csv"
		if _, err := os.Stat(dialog.FilePath); err == nil {
			if walk.MsgBox(w.MW, "覆盖文件", "文件已存在，确定覆盖吗？\n"+dialog.FilePath, walk.MsgBoxYesNo|walk.MsgBoxIconWarning|walk.MsgBoxDefButton2) != walk.DlgCmdYes {
				return
			}
		}
	}
	var buffer bytes.Buffer
	buffer.WriteString("\xef\xbb\xbf") // UTF-8 BOM for Chinese text in Excel.
	writer := csv.NewWriter(&buffer)
	writer.UseCRLF = true
	rows := [][]string{{"词语", "词性", "意思", "次数", "例句", "翻译"}}
	for _, entry := range w.entries {
		d := entry.Definition
		rows = append(rows, []string{d.Term, d.PartOfSpeech, d.MeaningZH, strconv.FormatUint(entry.Count, 10), d.ExampleEN, d.ExampleZH})
	}
	if err = writer.WriteAll(rows); err == nil {
		err = os.WriteFile(dialog.FilePath, buffer.Bytes(), 0600)
	}
	if err != nil {
		walk.MsgBox(w.MW, "TouchDict", "导出失败："+err.Error(), walk.MsgBoxIconError)
		return
	}
	walk.MsgBox(w.MW, "TouchDict", "查词记录已导出至：\n"+dialog.FilePath, walk.MsgBoxIconInformation)
}
