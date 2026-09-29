# TouchDict

Windows 11/10 轻量划词词典。选中英文后按 `Ctrl+Alt+D`，或在 Windows 触摸板设置中把“三指轻点”映射为“鼠标中键”。TouchDict 会调用 Gemini 返回上下文词义、词性、例句及翻译，并使用 Windows 美式英语语音朗读。

## 使用

1. 将 `gemini_key.txt` 与 `TouchDict.exe` 放在同一目录。
2. 运行 `TouchDict.exe`，程序常驻系统托盘。
3. 把鼠标悬停在英文单词上；也可以先划选一个单词或短语。
4. 三指轻点（Windows 映射为左 Alt）或按 `Ctrl+Alt+D`。没有现有选区时，TouchDict 会自动双击选择鼠标下的单词。
5. 托盘菜单可暂停监听、打开预览、设置密钥或退出。

## 界面预览

```powershell
TouchDict.exe --preview
TouchDict.exe --preview=loading
TouchDict.exe --preview=empty
TouchDict.exe --preview=error
TouchDict.exe --preview=edge
```

## 说明

- 取词使用临时复制并恢复原来的文本剪贴板内容。某些受保护应用、远程桌面或不允许复制的控件可能无法取词。
- Windows 不向普通程序直接提供 Precision Touchpad 三指轻点事件，请把“三指轻点”映射为左 Alt。鼠标中键不会被 TouchDict 监听或拦截。
- 密钥从设置（DPAPI 加密）、`GEMINI_API_KEY` 环境变量或 EXE 同目录文件读取。
- 发音依赖 Windows 安装的 `en-US` 语音。
