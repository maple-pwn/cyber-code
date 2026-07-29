# cyber-code 发行 Bundle

cyber-code 的发行 metadata 是一个自包含 JSON envelope。它可以与二进制一起部署到任意 HTTPS 静态文件服务，不依赖特定发布商。客户端只检查并报告新版本，不会下载、执行或安装 artifact。

## 密钥

Ed25519 私钥只能保存在受限文件或 GitHub Actions secret `CYBER_CODE_UPDATE_SIGNING_KEY` 中。仓库、日志、bundle 和产品二进制都不能包含私钥。

生成 32-byte seed 并导出公钥：

```bash
umask 077
openssl rand -base64 32 > signing-key.txt
CYBER_CODE_UPDATE_SIGNING_KEY=$(cat signing-key.txt) \
  go run ./scripts/release-manifest public-key
```

`public-key` 和 `manifest` 同时接受 `--key-file signing-key.txt`。key file 超过 4 KiB、不是严格 base64 或解码后不是 32-byte seed/64-byte Ed25519 private key 时会失败，错误不会回显密钥。

## 手动工作流

`.github/workflows/release-bundle.yml` 只支持手动 `workflow_dispatch`。运行时提供：

- `version`：不带 `v` 的 semantic version；
- `base_url`：最终托管 bundle 的 HTTPS 目录。

工作流构建 Linux、Windows 和 macOS amd64 二进制，将 version、`base_url/latest.json` 和公钥注入二进制，生成 `latest.json`，最后上传一个 GitHub Actions artifact。它不会创建 GitHub Release，也不会上传到外部站点。

`validate` 子命令会在 linker 注入前校验 semantic version 和 HTTPS base URL，并输出规范化的 base URL：

```bash
go run ./scripts/release-manifest validate \
  --version 2.2.0 \
  --base-url https://downloads.example.com/cyber-code/2.2.0
```

下载 workflow artifact 后，把目录中的所有文件原样上传到 `base_url`。`latest.json` 内的 URL 必须与实际文件位置一致，否则客户端虽能验签，但后续人工下载会失败。

## 本机构建

```bash
VERSION=2.2.0
BASE_URL=https://downloads.example.com/cyber-code/2.2.0
PUBLIC_KEY=$(go run ./scripts/release-manifest public-key --key-file signing-key.txt)

go build -trimpath -ldflags \
  "-X cyber-code/internal/product.BuildVersion=$VERSION \
   -X cyber-code/internal/product.UpdateMetadataURL=$BASE_URL/latest.json \
   -X cyber-code/internal/product.UpdatePublicKeyB64=$PUBLIC_KEY" \
  -o cyber-code ./cmd/cli
```

生成 manifest 时必须显式给出 publication time 和每个平台文件：

```bash
go run ./scripts/release-manifest manifest \
  --version "$VERSION" \
  --published-at 2026-07-29T00:00:00Z \
  --base-url "$BASE_URL" \
  --key-file signing-key.txt \
  --artifact linux/amd64=cyber-code-linux-amd64 \
  --artifact windows/amd64=cyber-code-windows-amd64.exe \
  > latest.json
```

相同 payload 和密钥总会得到相同 envelope；artifact 参数顺序不影响输出。

## 验证

产品检查保持显式 opt-in：

```bash
cyber-code version-check --enable
```

发行构建使用注入的 metadata URL/公钥；开发构建需显式传 `--metadata-url` 和 `--public-key`。输出包括选中平台 artifact 的 URL、SHA-256 和大小，不会创建下载文件。

离线审计可以先从 envelope 提取签名字节，再用 OpenSSL 验证 canonical payload：

```bash
jq -r .payload latest.json | base64 --decode > payload.json
jq -r .signature latest.json | base64 --decode > signature.bin
PUBLIC_KEY=$(cat update-public-key.txt)
printf '302a300506032b6570032100%s' "$(printf '%s' "$PUBLIC_KEY" | base64 --decode | xxd -p -c 256)" \
  | xxd -r -p > public-key.der
openssl pkeyutl -verify -pubin -keyform DER -inkey public-key.der \
  -rawin -in payload.json -sigfile signature.bin
```

再对照 `payload.json` 中的 `sha256` 和 `size` 校验每个 artifact。不同平台的 `base64` 参数可能不同，应使用本机等价的 decode 选项。

## 轮换

公钥固定在产品二进制中。轮换密钥时，应先发布同时信任新公钥的新客户端，或通过独立可信渠道发布使用新公钥构建的客户端；不能只替换静态站点上的 key 和 manifest。旧私钥撤销后应从 GitHub secret 与本地密钥库中删除，并保留不含私钥的审计记录。
