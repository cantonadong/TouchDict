# Gemini API 免费模型、额度与 TouchDict 使用说明

核对日期：2026-10-06。本文说明 Gemini Developer API，适用于 TouchDict 中配置的 Gemini API Key；不等同于 Gemini 网页聊天、Google AI 订阅或 Vertex AI。

## 1. 先回答：每天免费多少次、多少 Token？

**没有一个适用于所有模型、所有项目的统一数字。** 免费层级有多项限额，同时满足才能继续使用：

| 指标 | 中文含义 | 达到限制后如何处理 |
|---|---|---|
| RPM | 每分钟请求数 | 降低连续查询速度，稍后再查 |
| TPM | 每分钟输入 Token 数 | 减少输入和上下文，稍后再查 |
| RPD | 每天请求数 | 等待当天额度重置 |
| TPD（若该模型适用） | 每天 Token 数 | 按该项目显示的限制处理 |

任何一项超限都可能返回 429。限额按 **项目** 计算，同一个项目下换 API Key 不会获得一份新的额度。各模型限制可能不同，不能假设换模型一定得到独立额度；以项目后台显示为准。

RPD 在 **美国太平洋时间零点** 重置，不是从第一条请求起滚动计算 24 小时。换算为中国时间：美国夏令时期间通常是下午 15:00，冬令时期间通常是下午 16:00。2026-10-06 对应新西兰奥克兰时间约晚上 20:00。这里的换算只是方便理解，服务端以太平洋时间为准。

官方目前要求在登录后的 AI Studio 查看项目实际限额，公共文档没有给出保证适用于你的项目的固定免费数字。本文不使用网上旧的“每日 1,500 次”等数字来替代你的实际额度。

依据：[官方限额规则](https://ai.google.dev/gemini-api/docs/rate-limits)。

## 2. TouchDict 提供的免费层级查词模型

下面 10 个模型的标准文本输入、输出在官方价格表中均列有免费层级。**“支持免费层级”表示该模型有免费方案，不表示你的 Key 永远免费，也不表示每个项目都获准调用。**

| 设置与托盘中的模型 ID | 免费输入价格 | 免费输出价格 | 免费 RPM / TPM / RPD | TouchDict 使用说明 |
|---|---|---|---|---|
| `gemini-3.1-flash-lite` | 0 | 0 | 查看项目后台 | 当前默认；本机同样的查词请求已成功返回 |
| `gemini-3.5-flash-lite` | 0 | 0 | 查看项目后台 | 另一款 Flash-Lite，作为可切换选项 |
| `gemini-3.8-flash` | 0 | 0 | 查看项目后台 | Flash 系列 |
| `gemini-3.7-flash` | 0 | 0 | 查看项目后台 | Flash 系列 |
| `gemini-3.6-flash` | 0 | 0 | 查看项目后台 | Flash 系列 |
| `gemini-3.5-flash` | 0 | 0 | 查看项目后台 | Flash 系列 |
| `gemini-3-flash-preview` | 0 | 0 | 查看项目后台 | 预览版本；服务和限额可能调整 |
| `gemini-2.5-flash-lite` | 0 | 0 | 查看项目后台 | 旧系列，存在访问资格限制 |
| `gemini-2.5-flash` | 0 | 0 | 查看项目后台 | 旧系列，存在访问资格限制 |
| `gemini-2.5-pro` | 0 | 0 | 查看项目后台 | 旧系列，存在访问资格限制 |

表中的价格为免费层级下的标准文本输入、输出费用，不是 Token 数量。详细额度要逐个模型查看。支持列表按核对日期整理，未来模型发布、下线或价格变化后需要更新软件。

依据：[官方价格表的 Standard / Free Tier 栏](https://ai.google.dev/gemini-api/docs/pricing)。

Google 目前说明：2.5 系列的访问限于过去已积极使用这些模型的用户，新项目应使用较新的模型。菜单提供旧模型选项，但不会替你绕过访问限制。依据：[官方模型目录](https://ai.google.dev/gemini-api/docs/models)。

本机已验证的是 `gemini-3.1-flash-lite`，其余选项根据官方兼容性与免费层级资料列入，未逐个发起收费或配额消耗验证。模型列表接口能列出某模型，也不能据此判断你的实际可用额度。

旧的 `gemini-flash-lite-latest` 是版本别名。此前本机连续遇到 503 与超时，已迁移到固定的 `gemini-3.1-flash-lite`。固定版本便于确认实际选择；软件现在不会把你主动选择的 2.5 模型偷偷改为 3.1。

## 3. 如何查看你自己的准确额度

1. 用创建 API Key 的 Google 账号登录 [Google AI Studio](https://aistudio.google.com/)。
2. 在 [API Keys](https://aistudio.google.com/apikey) 页面找到 TouchDict 使用的 Key 所属项目，确认其 Billing Tier 是 Free 还是 Paid。
3. 打开 [Usage / Rate limits](https://aistudio.google.com/usage?tab=rate-limit)，选择同一个项目。若链接界面调整，可从左侧 Usage 页面进入 Rate limits。
4. 查看准备使用的模型对应的 RPM、TPM、RPD，以及页面列出的其他限额。
5. 用同一项目的用量页面对照已消耗的请求与 Token。限额与用量是两回事：前者是上限，后者是已经用了多少。

下面是给自己记录用的模板，不能把“尚未读取”理解为不限量：

| 模型 | RPM 上限 | 输入 TPM 上限 | RPD 上限 | TPD（若有） | 当前项目层级 |
|---|---|---|---|---|---|
| `gemini-3.1-flash-lite` | 尚未读取 | 尚未读取 | 尚未读取 | 以后台为准 | 以后台为准 |
| 其他准备使用的模型 | 尚未读取 | 尚未读取 | 尚未读取 | 以后台为准 | 以后台为准 |

TouchDict 目前只持有调用接口的 Key，没有你的 AI Studio 登录会话。普通模型列表接口不返回这份项目限额，因此不能自动给你填入准确数字。查看后台不需要把 Key 内容发给别人。

## 4. 免费层级与付费层级是什么关系？

| 情况 | 含义 |
|---|---|
| Free Tier | 在项目的免费限额内使用支持免费层级的模型 |
| Paid Tier | 项目已接入计费，按相应付费价格和限额使用 |
| 价格单位“每 100 万 Token” | 计费计量单位，不代表每天赠送 100 万 Token |
| Google AI Studio 页面可免费体验 | 不足以证明软件中的 API Key 所属项目也按免费层级计费 |

升级付费层级需要设置计费；预付费账户可能还需要充值。不要理解成“先把免费日额度用完，再自动按免费模型免单”。收费取决于项目层级及所调用服务，软件里的“支持免费层级”模型选项不会更改 Google 项目的计费状态。

免费额度耗尽不会由 TouchDict 自动替你开通付费或充值。付费项目也有速率限制。免费层级的输入和输出可能用于改进 Google 产品，付费服务的数据使用规则不同。

依据：[官方计费说明](https://ai.google.dev/gemini-api/docs/billing)、[服务条款](https://ai.google.dev/gemini-api/terms)。

## 5. Token、上下文窗口与每日额度要分开看

Token 是模型处理文本的单位，不是一个 Token 必然等于一个字或一个单词。输入 Token 包括实际发送的说明、词语和上下文；输出 Token 包括模型生成内容，某些模型还有思考 Token。精确数量可使用 `countTokens` 或响应中的 `usageMetadata` 获取。依据：[官方 Token 说明](https://ai.google.dev/gemini-api/docs/tokens)。

下面这些是 **单次模型能力上限**，不是每天免费赠送的数量：

| 模型 | 单次输入 Token 上限 | 单次输出 Token 上限 | 官方能力来源 |
|---|---|---|---|
| `gemini-3.1-flash-lite` | 1,048,576 | 65,536 | [模型页](https://ai.google.dev/gemini-api/docs/models/gemini-3.1-flash-lite) |
| `gemini-3.5-flash-lite` | 1,048,576 | 65,536 | [模型页](https://ai.google.dev/gemini-api/docs/models/gemini-3.5-flash-lite) |
| `gemini-3.8-flash` | 1,048,576 | 65,536 | [模型页](https://ai.google.dev/gemini-api/docs/models/gemini-3.8-flash) |

例如：即使某模型单次允许输入约 100 万 Token，如果你的项目输入 TPM 只有 20,000，也不能据此认定你可立即发送 100 万 Token；还必须符合项目限额。

TouchDict 的实际请求远小于上述窗口：查询词语最多取 300 个字符，上下文最多取 1,200 个字符，生成上限设置为 2,048 Token。它也会发送词典规则和 JSON 结构要求；只查询一个单词，实际输入仍不止这个单词。2,048 是生成上限，不代表每次一定消耗 2,048 Token。

软件每次查词是一条独立请求，没有将整个查询历史当作聊天上下文发送。HTTP 请求超时为 18 秒，查询协调层的总体期限为 20 秒；两者都是软件等待规则，不是免费服务只有这点使用时间。

## 6. 用例：为什么有时查不了？

以下数字 **仅用于讲解，不是你的真实额度**。假设模型限额为 RPM=5、输入 TPM=20,000、RPD=100：

- 在一分钟内连续发起第 6 条请求，会碰到 RPM，即使当天只查了 6 次。
- 一分钟内只发了 2 条请求，但输入累计 25,000 Token，仍可能碰到 TPM。
- 每次间隔很久，当天第 101 条请求仍会碰到 RPD，需要等日额度重置。
- 每条请求都返回很短的释义，也不能用较少的输出 Token 抵消已经耗尽的请求数。

再假设 100 条成功请求平均每条输入 600 Token、输出 150 Token，总量约为输入 60,000、输出 15,000 Token。这只是按假设平均值计算的估算；不能把它当作 Google 规定的“每天免费输入/输出额度”。

TouchDict 的历史次数记录的是成功查询次数，包含缓存命中。API 限额衡量的是发给 Google 的请求和 Token，所以 CSV 的“次数”不能用来推算准确 API 用量。点击历史只读取已保存结果；本地缓存命中也无需发给 Google，因此通常不消耗新的 API 查询额度。

## 7. 模型快速切换怎么使用？

右键点击系统托盘的 TouchDict 图标，进入“模型快速切换：当前模型”子菜单。当前模型带勾，点击另一项后保存配置，下次查询使用该模型，重启后保留。设置窗口里的模型选项使用同一份列表；从设置保存后，托盘显示同步更新。

切换不会重复请求正在进行的查询，也不会删除历史或缓存。已经命中本地缓存的查询仍显示已保存结果，因此切换后重查同一个词，不一定马上发生新的联网请求。

如果切换后报 403、404 或 429，应查看该项目对相应模型的权限与额度；不要根据菜单中的名称判断 Key 一定有权限。503 和超时表示另一类问题，不能直接解释成每日免费额度耗尽。

依据：[官方 API 错误说明](https://ai.google.dev/gemini-api/docs/api-errors)。

## 8. 其他有免费层级的模型为何不放进查词下拉框？

Gemini API 不只有文本查词模型。当前价格表还列有以下免费层级服务；它们使用不同输出或接口，不适合直接替代 TouchDict 的词典 JSON 请求：

| 类别 | 有免费层级的模型 ID |
|---|---|
| 实时音频 | `gemini-3.8-live`、`gemini-3.8-live-extended-thinking`、`gemini-3.1-flash-live-preview`、`gemini-2.5-flash-native-audio-preview-12-2025` |
| 语音翻译 / 转写 | `gemini-3.5-live-translate-preview`、`gemini-3.5-transcribe-live`、`gemini-3.5-transcribe` |
| 文本转语音 | `gemini-3.8-flash-tts`、`gemini-3.8-flash-lite-tts`、`gemini-3.1-flash-tts-preview`、`gemini-2.5-flash-preview-tts` |
| 向量嵌入 | `gemini-embedding-2` |
| 机器人空间推理 | `gemini-robotics-er-2-preview`（专门用途，未接入 TouchDict） |

图像、视频、音乐以及其他 Pro 型号应逐个看价格表；不要从“Google AI Studio 能体验”推导出 API 也免费。本文的查词模型列表不包含价格表没有明确列出免费标准文本调用方案的型号或不明确指向固定版本的别名。

依据：[官方价格表](https://ai.google.dev/gemini-api/docs/pricing)。
