TouchDict – Figma 可用切片包

推荐用法：
1. 将 TouchDict_Full_UI.svg 直接拖入 Figma，可作为整页参考与继续编辑基础。
2. 01–08 为独立组件切片，适合直接拖入 Figma 后建立 Component。
3. design_tokens.json 包含画布、颜色、圆角、间距和字号规范。

说明：
- 这里优先使用 SVG，而不是 PNG 切片，因为 SVG 在 Figma 中缩放不会失真，也更适合继续拆解/编辑。
- 中文字体使用系统 fallback：Inter / Segoe UI / Arial。macOS 侧可改为 SF Pro；Windows 侧建议 Segoe UI。
- 阴影采用 SVG filter，Figma 导入后若样式解析不同，可按 full UI 视觉重新设置 Effect。
