# TouchDict 订阅授权查词设计

## 用户目标

用户在 TouchDict 点击授权，系统浏览器直接打开 OpenAI 页面。用户确认账号并同意授权后，应用自动获取结果，显示“已连接”，即可使用个人订阅查词。后续启动恢复连接，尽可能自动续期。

## 接入选择

采用 OpenAI 为开源应用提供的 Sign in with ChatGPT 授权流程，直接调用 Responses API。无需用户安装 Codex CLI、输入 API Key 或复制授权码。现有在线客户端已经提供查词入口，但授权、凭据管理和流式 Responses 查询属于新增模块。

与通过本机 Codex app-server 相比，直接接入减少运行依赖；应用负责授权验证和续期。实际使用资格、可用模型和额度由 OpenAI 决定，登录成功不能代表模型请求必然成功。

## 用户交互

设置新增“ChatGPT / Codex 订阅”标签，包含授权按钮、连接状态、当前账号、模型选择以及断开连接入口。授权按钮同时表达“Continue with ChatGPT / 点击授权”。

状态：未连接 → 等待浏览器授权 → 已连接。拒绝、超时、浏览器打开失败显示具体原因并允许重试。已连接仅在完成回调、身份验证、权限检查和凭据保存后显示。

首次成功连接自动选择该订阅提供方并持久保存，使关闭设置后即可查词；按钮本身明确表明授权后启用订阅查词。授权是即时账户操作，设置的取消按钮不撤销已完成授权。模型选择继续沿用设置保存行为。

可复制本次授权链接作为浏览器打开失败时的备用操作。授权链接按登录尝试生成，不能用固定链接替代。

支持选择已保存账号或添加另一个账号；每个注册记录独立保存凭据，切换成功前保持原活动账号。断开当前连接停止使用该账号并尝试撤销可续期会话，保留注册映射供重新授权。远端撤销失败须明确告知用户。

## 授权与存储

新增 internal/chatgpt 模块负责 OAuth、身份验证、会话存储及 Responses 查词，设置层只负责交互和模型配置。

启动仅绑定 127.0.0.1 的临时端口回调服务，再生成随机 state、nonce、PKCE verifier 和 S256 challenge，打开官方授权页面。回调路径固定为 /auth/callback，授权请求与换码请求使用完全相同的地址。每次登录最多等待五分钟；关闭设置或取消授权会终止该次尝试。

首次注册使用 dynamic_agent_client 和稳定的本机 host ID、应用名称 TouchDict；回调取得已分配的 client ID 后用其换码并保存。再次授权复用该注册 client ID。校验 state，拒绝重复或无关回调；验证 ID token 的签名、issuer、audience、expiry、nonce 和返回账号身份，检查订阅使用权限。通过官方 OIDC discovery 获取签名验证资料。

注册身份、client ID、令牌、权限和到期时间按账号记录保存。使用 Windows DPAPI 加密，原子替换存储文件，目录为用户配置目录中的 TouchDict。host ID 独立持久化。令牌不进入源代码、日志、浏览器存储或查询错误文本。

查询前接近令牌到期时串行刷新，同一会话只能有一次刷新，原子保存替换后的令牌。网络临时失败保留会话并允许重试；明确授权失效才提示重新连接。重新启动恢复保存会话，不能仅凭文件存在宣称远端授权有效。

## 查询接入

新增订阅 provider，沿用现有 Selection → QueryResult、查词提示词、JSON 结果校验、缓存及历史流程。配置持久化当前 provider 和模型，托盘菜单同步新增订阅模型选项。活动账号通过会话管理器获取，不能作为 API Key 存入现有 key 字段。

模型候选从官方接口获取，并保存最近成功的列表用于重启显示；列表获取失败显示可理解的错误。模型列表不能证明请求权限，实际查询失败单独反馈。优先选择列表中适合简短文本查询的模型，不硬编码未经验证的当前型号。

使用 HTTP/SSE Responses，请求包含 store=false、stream=true、instructions 和 input 数组。按订阅接口要求省略不支持的参数。汇集文字并等待完整成功事件后解析词典 JSON，失败、截断或取消不写入缓存。处理超时、认证失效、额度限制、模型无权限和无效输出。

新增查询客户端接入 cmd/touchdict 的 client factory。现有全模型测速不会自动消费订阅额度；用户显式选择订阅模型测速时才发起请求。

## 验收与交付

遵守用户要求：不运行自动化测试。在 main 分支工作，保留工作区原有修改。进行编译和 diff 检查；终止正在运行的 TouchDict 并覆盖同一个 artifacts/TouchDict.exe，交付路径供用户手动授权和查词验证。

用户手动验收：点击授权打开页面；完成后显示已连接并能查词；关闭重启仍保持会话；拒绝授权可重试；授权失效提示重连；断开连接后不继续使用订阅。实际授权由用户在 OpenAI 页面完成。

## 官方依据

- https://developers.openai.com/siwc/quickstart
- https://developers.openai.com/siwc/token-sharing-open-source/sign-in
- https://developers.openai.com/siwc/token-sharing-open-source/profiles-and-sessions
- https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference
- https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations
