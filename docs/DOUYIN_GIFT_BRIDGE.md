# 抖音 WSS 礼物监听（浏览器辅助模式）


## 推荐：Go 面板扫码登录 + 后台 Chrome 多房间采集

基于用户上传的 douyin-live-gift-collector，本项目提供可选的 **Node.js 22+ + Chrome/Edge** 无窗口采集器。保留其独立持久化 profile、可见扫码窗口、CDP 防后台节流与失效自恢复方案；礼物的 protobuf 解码、msgId 去重、连击聚合、日志和 SSE 输出仍交给 Go。**无需单独部署 8787 礼物服务，也不必安装 Chrome 扩展。**

使用前：Node.js 必须是 22 或以上（不需要 npm 依赖）；Go 与浏览器在同一台电脑上。扫码需要图形桌面；纯无 GUI Docker 无法提供登录窗口。

1. 在「直播平台与规则 → 抖音」保存并启用至少一个房间；可在多房间设置增加其他房间。
2. 点击 **打开抖音扫码登录**；Go 启动可见的独立 Chrome/Edge profile。扫码或网页登录后，检测到有效 sessionid/sessionid_ss 则关闭窗口，并将登录态留在 Go 用户目录下的 douyin-collector/profile 子目录。
3. 点击 **启动后台礼物采集**；Node 在同一台机器的 headless Chrome 中打开已启用的每个抖音房间，注入只读的 WSS Hook，通过受 Go 临时令牌保护的本机接口转发**二进制原始 PushFrame**。
4. Web 页面可刷新采集器状态，查看登录 Cookie 是否存在、房间网页标题、IM 礼物帧数、成功转发数与错误数。按「获取主直播间信息」，从抖音房间详情 HTTP 接口读取标题、主播昵称、房间 ID、状态、观看数；不是从浏览器私密 Cookie 导出。
5. 点击 **停止采集/登录** 即可请求 Node 关闭其启动的 Chrome。登录过期时重新点击扫码登录，无需手动复制 Cookie。

### 后端接口
- GET /api/douyin/collector/status：本机管理员可读取运行状态、登录检测和房间概况；没有 Cookie 或配对令牌。
- POST /api/douyin/collector/action：JSON action 为 login/start/stop，需本机管理员权限。
- GET /api/douyin/room/info?instance_id=douyin：读取已启用主直播间的公开状态与主播信息。接口可能受抖音网站调整影响。
- POST /api/douyin/bridge/frame：由 Node 或扩展发送原始 protobuf 帧，临时 token + 回环地址限制。

**风险和边界**：Chrome 远程调试只绑定 127.0.0.1，并在启动时分配随机端口。独立 Chrome profile 中可能包含会话凭据，切勿分享；Go 不在响应中回传它们。Cookie 存在只能说明会话基础状态，不等同于抖音认证接口已验证成功。浏览器来源礼物不作为可认证的付费资格，也不自动生成礼物排队积分。

上传项目的 gift_server.mjs 采用不要求身份认证的本地 JSON 接口；本项目不依赖该服务，改为 Go 解析 PushFrame 并做去重与连击结算。超时兜底采用 **10 秒**，有 repeatEnd 时立即结算。Node 22 与 Chrome 不是 Go 可执行文件的一部分，需要额外安装；手动打包脚本将采集器 JS 文件放在 tools/douyin-gift-collector。

### 可选的扩展模式

下文保留原来的 Chrome 扩展方式，适合不想安装 Node 的用户；扩展与后台采集器**任选一种**即可，不推荐同时重复监控同一个直播间。



此功能针对 BiliPDJ-GO 已有抖音 HTTP 弹幕监听新增 **礼物观测通道**；不会替换现有 HTTP 拉取器，也不需要手动生成 `signature` 或 `a_bogus`。使用者须在 Chrome/Chromium 的 **已登录** 抖音直播网页中打开对应直播间。

## 安装

1. 启动 BiliPDJ-GO，在「平台与规则」中启用抖音监听并填入完整直播间号/链接（默认本地面板 `http://127.0.0.1:9816/`）。
2. 在 Chrome 打开 `chrome://extensions/`，启用「开发者模式」，点击「加载已解压的扩展程序」，选择仓库中的 `tools/douyin-gift-extension/` 文件夹。
3. 在运行 BiliPDJ-GO 的**同一台电脑**上，通过浏览器访问 `http://127.0.0.1:9816/api/douyin/bridge/token`。复制返回 JSON 中的 `token`。此接口仅限本机管理请求，令牌在每次 Go 后端重启后更换。
4. 进入浏览器扩展的「选项」，填写 Go 端口（默认 9816）、抖音监听实例 ID（主房间为 `douyin`）、配对令牌，保存。
5. 打开或**刷新** `https://live.douyin.com/<live_id>`，确保已登录并且该直播间与 Go 里启用的直播间一致。等待实际礼物消息；Go 日志会显示 `[douyin] ... 送出 ...（...钻，浏览器观测）`。
6. 可以访问 `http://127.0.0.1:9816/api/douyin/bridge/status` 检查绑定的直播间、累计已结算事件和最后接收时间（仅本机管理请求）。

## 多抖音房间

BiliPDJ-GO 原有的额外监听实例 ID 如 `douyin-mroom001` 不变。扩展「选项」中的多房间映射每行一个：
```text
980468525827=douyin
730127423738=douyin-mroom001
```
左侧必须是浏览器地址栏 `live.douyin.com/` 后面的 ID，右侧是 Go 已启用的监听实例 ID。一个扩展可同时桥接多个打开的直播标签页。与现有 HTTP 弹幕监听并行运行；不会合并不同直播间的连击。

## 数据流和安全

- 扩展在网页 MAIN world 拦截浏览器原有的 `/webcast/im/push/` WebSocket **接收事件**，只把包含 `WebcastGiftMessage` 的二进制帧发送给扩展后台。
- 扩展后台请求 `POST http://127.0.0.1:<端口>/api/douyin/bridge/frame`，通过 `X-PDJ-Bridge-Token` 配对；此 API **只接受回环地址**、已启用的抖音直播实例、匹配的网页直播间 ID，并对请求体和解码大小设限。
- Go 使用自己的 Protobuf/Gzip 解码器，不依赖扩展上传礼物 JSON。原始单包 msgId 去重；同一连击累计数量取最大值；收到 `repeatEnd=1` 结算，或在 10 秒无新包时超时结算。
- 浏览器页面可以伪造或重放可见数据；**本通道绝不授予付费礼物排队资格**。真实支付/收益统计必须使用可认证的服务端/平台渠道。日志中的钻数是推送中标注的礼物价值，不是主播实际到账。
- 配对令牌只保存在扩展存储中，不出现在抖音页面；不要分享令牌或自行暴露本机接口。重启 Go 后需重新配对。
- 只有 Chromium MV3 扩展得到测试范围内的实现。Tampermonkey 用户脚本不是此桥接方案的完整替代。

## 限制

本功能尚未实现绕过网页登录的原生 WSS 鉴权、扫码登录、独立签名生成，也没有在真实线上房间完成压力/兼容性验收。抖音页面和 protobuf 字段随时可能变化；匿名 HTTP 模式未必能拿到礼物，但不会因此影响原有 HTTP 弹幕流程。远端/Docker 后端若不在浏览器同一台机器的回环地址上，需要**自行设计安全的本地端口转发**，不得直接放开公开访问权限。
