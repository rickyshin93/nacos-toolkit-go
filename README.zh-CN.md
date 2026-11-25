# Nacos Toolkit for Go

[English](README.md)

[![CI](https://github.com/rickyshin93/nacos-toolkit-go/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/rickyshin93/nacos-toolkit-go/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/rickyshin93/nacos-toolkit-go.svg)](https://pkg.go.dev/github.com/rickyshin93/nacos-toolkit-go)

通过 Nacos HTTP OpenAPI v1 或本地文件读取配置。支持占位符展开、覆盖配置深合并和 Nacos 批次缓存。配置行为参考 `nacos-toolkit-rs` 的 `4ab8f42` 版本。

## 安装

在调用方项目的 Go module 目录运行以下命令。

```sh
go get github.com/rickyshin93/nacos-toolkit-go@v0.0.1
```

## 从 Nacos 读取配置

```go
import (
    "context"

    toolkit "github.com/rickyshin93/nacos-toolkit-go"
)

func load(ctx context.Context, conn toolkit.Connection) (any, error) {
    manager, err := toolkit.New(conn)
    if err != nil {
        return nil, err
    }

    base := []toolkit.ConfigRef{
        {DataID: "common.yml", Group: "DEFAULT_GROUP"},
        {DataID: "app.yml", Group: "DEFAULT_GROUP"},
    }
    result, err := manager.Get(ctx, base, nil, false)
    return result.Config, err
}
```

后续读取同一批次时复用 Manager。每个独立连接或独立缓存使用各自的 Manager。

- `Connection.ServerAddr` 接受主机和端口或 HTTP(S) origin。`Namespace` 为空时省略 `tenant` 参数。用户名或密码任一非空时，首次读取会请求 `/nacos/v1/auth/login`。登录成功后，该配置源持续复用 token。每次 HTTP 请求超时为 10 秒，并支持调用方通过 context 取消。
- `Get` 按顺序读取基础配置并逐份按 YAML 解析。各份配置的顶层字段构成变量上下文，同名字段以后读到的为准。最后一份基础配置作为输出。外部变量 `DEPLOY_ENV` 取命名空间，待渲染配置自身的字段优先。
- 可传入覆盖配置引用，在基础批次之后读取。以 `.json` 结尾的覆盖配置按 JSON 解析。覆盖配置自身的字段优先于已渲染的基础配置和外部变量。对象递归合并，数组和标量整体替换。
- 每个 Manager 缓存首次成功的批次结果，之后复用该结果。Debug 模式返回未经渲染的基础变量上下文。`ClearCache` 清除处理结果和原始上下文，并保留配置源的登录 token。并发首次读取会串行处理。
- 请求失败会中断批次并返回错误。可以用 `errors.As` 识别 `*toolkit.HTTPError`。读取覆盖配置失败后仍可通过 `Raw` 获取基础上下文，处理结果要等成功后才会缓存。

## 处理配置与查找本地文件

```go
config := toolkit.Process(
    "host: ${HOST}\nport: 3000",
    toolkit.YAML,
    map[string]any{"HOST": "localhost"},
    nil,
)

path, found := toolkit.FindLocalConfig("app", "./config")
if found {
    local, err := toolkit.ParseConfigFile(path)
    // 使用 local 或处理 err。
    _, _ = local, err
}
```

- 解析后的配置使用 `map[string]any`、`[]any` 和 JSON 标量，数字使用 `json.Number`。`Parse` 和 `Process` 遇到无效内容或 YAML 根节点为非对象时返回空对象。JSON 支持任意根值。`ParseConfigFile` 直接返回解析错误，YAML 和 JSON 文件均支持标量和数组根值。
- 本地查找顺序为 `.json`、`.yaml`、`.yml`。优先命中的文件解析失败时，`GetLocalConfig` 返回 `false`。显式文件路径的扩展名支持大小写。
- `Render` 和 `Process` 展开 `${path.to.key}`。整值引用保留目标值类型。数组中的直接字符串元素使用文本替换，嵌套数组原样复制。缺失或为空值的变量保留占位符。
- `RenderText` 最多替换五轮。完整渲染会解析引用链并在遇到循环时终止。整值引用的容器展开最多五层。
- 默认将 `cors.whitelist` 中逗号分隔的字符串拆成数组，并去除各项首尾空白。传入非 nil 的点路径列表可指定转换字段，空列表关闭转换。`ProcessAndMerge` 可对本地值应用相同的覆盖规则。

## 开发与检查

使用 Go **1.27.4**，这是 2026-11-25 核对的最新稳定补丁版本。唯一运行时依赖是 YAML 组织维护的 **go.yaml.in/yaml/v4 v4.0.0**。解析时通过 YAML 节点保留参考实现的标量和映射语义。 [Go 发布记录](https://go.dev/doc/devel/release) · [go-yaml v4.0.0](https://github.com/yaml/go-yaml/releases/tag/v4.0.0)

```sh
make check
```

命令依次检查 `gofmt`、`go vet ./...`、`go test -race -count=1 ./...` 和 `go build ./...`。存在项目本地的 `.local/go` 时使用它，其余情况使用 `PATH` 中的 Go。测试包括 Rust/Python crosscheck fixture，以及用 `httptest` 验证 URL 编码、可选鉴权、读取顺序、缓存、响应错误和请求取消。

真实 Nacos 只读测试使用已授权的环境参数。设置 `NACOS_ADDR`、`NACOS_DATA_ID`、`NACOS_GROUP`，按需设置 `NACOS_NS`、`NACOS_USER`、`NACOS_PASS`，然后运行 `make test-live`。测试验证返回值可解析为配置对象，日志只报告测试结果。

## 发布版本

运行 `go mod tidy` 和 `make check`，将改动合入 `main`。GitHub Actions 会在 Linux 和 macOS 上检查 PR 和 `main`。待要发布的提交通过这两项检查后，创建并推送新的语义化版本标签。以 `v0.0.2` 为例。

```sh
git switch main
git pull --ff-only origin main
git tag -a v0.0.2 -m "Release v0.0.2"
git push origin v0.0.2
GOPROXY=https://proxy.golang.org go list -m github.com/rickyshin93/nacos-toolkit-go@v0.0.2
```

公开 Git 标签就是 Go module 版本。代理抓取后，该版本也可被 pkg.go.dev 收录。GitHub Actions 会对标签重新运行两项检查，通过后自动创建带变更说明的 GitHub Release。发布后有改动时，推送一个新版本标签。 [Go 官方发布指南](https://go.dev/doc/modules/publishing)

## 兼容性边界

Go API 使用显式 Manager 实例、调用方传入的 context 和标准 Go 错误。传输层提供 Nacos HTTP OpenAPI v1 配置读取。可能持续扩张的文本循环会终止并保留未解析的占位符。JSON 中未配对的 Unicode 代理转义遵循 Go 标准库行为，可能输出替换字符，Rust 解析器会报错。

## 许可证

MIT。
