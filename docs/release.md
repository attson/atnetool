# ATNetool 发布与自动更新

ATNetool 使用 GitHub Release 分发桌面包。客户端只安装同时通过 Ed25519 发布签名和 SHA-256 文件校验的更新。

## 首次配置

在 GitHub 仓库创建 `prod` Environment，并配置两个 secrets：

- `ATNETOOL_UPDATE_VERIFY_PUBLIC_KEY`：Ed25519 SPKI PEM 公钥。
- `ATNETOOL_UPDATE_SIGNING_PRIVATE_KEY`：与公钥配对的 Ed25519 PKCS8 PEM 私钥。

私钥必须离线备份，不能提交到仓库。可在可信本机生成密钥：

```bash
node -e "const c=require('crypto');const {publicKey,privateKey}=c.generateKeyPairSync('ed25519');console.log(publicKey.export({type:'spki',format:'pem'}).trim());console.log(privateKey.export({type:'pkcs8',format:'pem'}).trim())"
```

## CI 与发布

- 推送或提交 PR 到 `main`：运行 Go 测试与前端构建。
- 手动运行工作流：构建五个平台并保留 14 天 artifact，不创建 Release。
- 推送 `vX.Y.Z` tag：构建、生成 `SHA256SUMS`、签名并发布正式 GitHub Release。

```bash
git tag v0.1.0
git push origin v0.1.0
```

发布矩阵：

- macOS arm64 / amd64：`.app.tar.gz`
- Windows amd64：安装器 `.exe` 与自动更新 `.zip`
- Linux arm64 / amd64：`.tar.gz`

CI 会把 tag 版本注入客户端和安装包元数据，无需在发布前手动修改源码版本。

## 客户端行为

正式构建启动五秒后静默检查最新 Release。发现新版本后才显示更新状态条。下载时会依次验证：

1. `SHA256SUMS.sig` 是否为发布私钥签名；
2. 当前平台资产是否存在于 `SHA256SUMS`；
3. 下载文件的 SHA-256 是否完全一致。

任一验证失败都不会替换现有应用。开发构建未注入公钥，自动更新默认关闭。
