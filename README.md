# mailkit

`mailkit` 是一个 Go 临时邮箱工具库，提供统一的 provider 注册、实例化、邮箱创建、邮件内容轮询和多 provider 路由能力。

模块路径：

```go
github.com/gopkg-dev/mailkit
```

## 功能

- 统一的 `Provider` 接口
- 内置多种临时邮箱 provider
- 基于注册表的 provider 发现与创建
- 支持邮件内容轮询
- 支持 `round_robin`、`random`、`failover` 路由策略
- 支持结构化 provider 配置

## 安装

```bash
go get github.com/gopkg-dev/mailkit
```

按需引入内置 provider：

```go
import (
	_ "github.com/gopkg-dev/mailkit/providers/cloudflaretemp"
	_ "github.com/gopkg-dev/mailkit/providers/duckmail"
	_ "github.com/gopkg-dev/mailkit/providers/mailtm"
	_ "github.com/gopkg-dev/mailkit/providers/moemail"
	_ "github.com/gopkg-dev/mailkit/providers/tempmaillol"
)
```

## 快速开始

### 1. 创建 provider

```go
package main

import (
	"context"
	"fmt"
	"log"

	mailkit "github.com/gopkg-dev/mailkit"
	_ "github.com/gopkg-dev/mailkit/providers/mailtm"
)

func main() {
	provider, err := mailkit.NewProvider("mailtm", mailkit.ProviderConfig{
		"api_base": mailkit.StringValue("https://api.mail.tm"),
	}, mailkit.FactoryDependencies{})
	if err != nil {
		log.Fatal(err)
	}

	mailbox, err := provider.CreateMailbox(context.Background(), mailkit.CreateMailboxInput{
		MailboxPrefix: "customprefix",
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("email:", mailbox.Email)
	fmt.Println("credential:", mailbox.Credential)
}
```

### 2. 轮询邮件内容

```go
ctx := context.Background()

content, err := provider.WaitForContent(ctx, mailkit.WaitForContentInput{
	Email:      mailbox.Email,
	Credential: mailbox.Credential,
})
if err != nil {
	log.Fatal(err)
}

fmt.Println("content:", content)
```

### 3. 路由多个 provider

```go
mailtmProvider, err := mailkit.NewProvider("mailtm", mailkit.ProviderConfig{
	"api_base": mailkit.StringValue("https://api.mail.tm"),
}, mailkit.FactoryDependencies{})
if err != nil {
	log.Fatal(err)
}

tempmailProvider, err := mailkit.NewProvider("tempmail_lol", mailkit.ProviderConfig{
	"api_base": mailkit.StringValue("https://api.tempmail.lol"),
}, mailkit.FactoryDependencies{})
if err != nil {
	log.Fatal(err)
}

router, err := mailkit.NewRouter(mailkit.Config{
	Strategy: "round_robin",
	Providers: []mailkit.Provider{
		mailtmProvider,
		tempmailProvider,
	},
})
if err != nil {
	log.Fatal(err)
}

selected, err := router.NextProvider()
if err != nil {
	log.Fatal(err)
}

fmt.Println("provider:", selected.Name())
```

## 核心接口

```go
type Provider interface {
	Name() string
	CreateMailbox(ctx context.Context, input CreateMailboxInput) (Mailbox, error)
	WaitForContent(ctx context.Context, input WaitForContentInput) (string, error)
	TestConnection(ctx context.Context, input CreateMailboxInput) error
}
```

## 内置 provider

### `mailtm`

- 字段：`api_base`、`debug`（可选）
- 用途：接入 Mail.tm

### `moemail`

- 字段：`api_base`、`api_key`、`debug`（可选）
- 用途：接入 MoeMail

### `duckmail`

- 字段：`api_base`、`bearer_token`、`domain`（可选）、`debug`（可选）
- 用途：接入 DuckMail

### `cloudflare_temp_email`

- 字段：`api_base`、`admin_password`、`domains`、`domain_strategy`、`debug`（可选）
- 用途：接入 Cloudflare Temp Email 服务端

`domain_strategy` 支持：

- `random`
- `round_robin`

### `tempmail_lol`

- 字段：`api_base`、`debug`（可选）
- 用途：接入 TempMail.lol v2 inbox API

## 配置方式

单值配置：

```go
config := mailkit.ProviderConfig{
	"api_base": mailkit.StringValue("https://api.mail.tm"),
}
```

多值配置：

```go
config := mailkit.ProviderConfig{
	"domains": mailkit.StringsValue(
		"alpha.example.com",
		"beta.example.com",
	),
}
```

读取配置：

```go
apiBase := config.GetString("api_base")
domains := config.GetStrings("domains")
strategy := config.GetStringOr("domain_strategy", "random")
debug := config.GetBool("debug")
```

## 路由策略

- `round_robin`：按顺序轮换 provider
- `random`：随机选择 provider
- `failover`：优先选择失败次数更少的 provider

## 测试

```bash
go test ./...
```
