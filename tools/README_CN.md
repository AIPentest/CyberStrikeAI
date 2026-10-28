# 工具配置文件说明

## 概述

每个工具都有独立的配置文件，存放在 `tools/` 目录下。这种方式使得工具配置更加清晰、易于维护和管理。系统会自动加载 `tools/` 目录下的所有 `.yaml` 和 `.yml` 文件。

## 配置文件格式

每个工具配置文件是一个 YAML 文件。下表列出了当前支持的顶层字段及其必填情况，建议逐项核对后再提交：

| 字段 | 必填 | 类型 | 说明 |
|------|------|------|------|
| `name` | ✅ | string | 工具唯一标识，建议使用小写字母、数字、短横线组合。 |
| `command` | ✅ | string | 实际执行的命令或脚本名称，需位于系统 PATH 或写入绝对路径。 |
| `enabled` | ✅ | bool | 是否注册到 MCP；设为 `false` 时该工具会被忽略。 |
| `description` | ✅ | string | 详细描述，支持多行 Markdown，供 AI 深度理解及 `resources/read` 查询。 |
| `short_description` | 可选 | string | 20-50 字摘要，用于工具列表、减少 token 消耗；缺失时会自动截取 `description` 开头。 |
| `args` | 可选 | string[] | 固定参数，按顺序 prepend 到命令行，常用于定义默认扫描模式。 |
| `parameters` | 可选 | array | 运行时可配置参数列表，详见「参数定义」章节。 |
| `arg_mapping` | 可选 | string | 参数映射模式（`auto`/`manual`/`template`），默认 `auto`；除非有特殊需求，无需填写。 |

> 若某字段填写错误或漏填必填项，系统会在加载时跳过该工具并在日志中输出警告，但不会影响其他工具。

## 工具描述

### 简短描述 (`short_description`)

- **用途**：用于工具列表，减少发送给大模型的token消耗
- **要求**：一句话（20-50字）说明工具的核心用途
- **示例**：`"网络扫描工具，用于发现网络主机、开放端口和服务"`

### 详细描述 (`description`)

支持多行文本，应该包含：

1. **工具功能说明**：工具的主要功能
2. **使用场景**：什么情况下使用这个工具
3. **注意事项**：使用时的注意事项和警告
4. **示例**：使用示例（可选）

**重要说明**：
- 工具列表发送给大模型时，使用 `short_description`（如果存在）
- 如果没有 `short_description`，系统会自动从 `description` 中提取第一行或前100个字符
- 详细描述可以通过 MCP 的 `resources/read` 接口获取（URI: `tool://tool_name`）

这样可以大幅减少token消耗，特别是当工具数量很多时（如100个工具）。

## 参数定义

每个参数可以包含以下字段：

- `name`: 参数名称
- `type`: 参数类型（string, int, bool, array）
- `description`: 参数详细描述（支持多行）
- `required`: 是否必需（true/false）
- `default`: 默认值
- `flag`: 命令行标志（如 "-u", "--url", "-p"）
- `position`: 位置参数的位置（整数，从0开始）
- `format`: 参数格式（"flag", "positional", "combined", "template"）
- `template`: 模板字符串（用于 format="template"）
- `options`: 可选值列表（用于枚举类型）

### 参数格式说明

- **`flag`**: 标志参数，格式为 `--flag value` 或 `-f value`
  - 示例：`flag: "-u"` → `-u http://example.com`
  
- **`positional`**: 位置参数，按顺序添加到命令中
  - 示例：`position: 0` → 作为第一个位置参数
  
- **`combined`**: 组合格式，格式为 `--flag=value`
  - 示例：`flag: "--level"`, `format: "combined"` → `--level=3`
  
- **`template`**: 模板格式，使用自定义模板字符串
  - 示例：`template: "{flag} {value}"` → 自定义格式

### 特殊参数

#### `additional_args` 参数

`additional_args` 是一个特殊的参数，用于传递未在参数列表中定义的额外命令行选项。这个参数会被解析并按空格分割成多个参数。

**使用场景：**
- 传递工具的高级选项
- 传递未在配置中定义的参数
- 传递复杂的参数组合

**示例：**
```yaml
- name: "additional_args"
  type: "string"
  description: "额外的工具参数，多个参数用空格分隔"
  required: false
  format: "positional"
```

**使用示例：**
- `additional_args: "--script vuln -O"` → 会被解析为 `["--script", "vuln", "-O"]`
- `additional_args: "-T4 --max-retries 3"` → 会被解析为 `["-T4", "--max-retries", "3"]`

**注意事项：**
- 参数会被按空格分割，但保留引号内的内容
- 确保参数格式正确，避免命令注入风险
- 此参数会追加到命令末尾

#### `scan_type` 参数（特定工具）

某些工具（如 `nmap`）支持 `scan_type` 参数，用于覆盖默认的扫描类型参数。

**示例（nmap）：**
```yaml
- name: "scan_type"
  type: "string"
  description: "扫描类型选项，可以覆盖默认的扫描类型"
  required: false
  format: "positional"
```

**使用示例：**
- `scan_type: "-sV -sC"` → 版本检测和脚本扫描
- `scan_type: "-A"` → 全面扫描

**注意事项：**
- 如果指定了 `scan_type`，会替换工具配置中的默认扫描类型参数
- 多个选项用空格分隔

### 参数描述要求

参数描述应该包含：

1. **参数用途**：这个参数是做什么的
2. **格式要求**：参数值的格式要求（如URL格式、端口范围格式等）
3. **示例值**：具体的示例值（多个示例用列表展示）
4. **注意事项**：使用时需要注意的事项（权限要求、性能影响、安全警告等）

**描述格式建议：**
- 使用 Markdown 格式增强可读性
- 使用 `**粗体**` 突出重要信息
- 使用列表展示多个示例或选项
- 使用代码块展示复杂格式

**示例：**
```yaml
description: |
  目标IP地址或域名。可以是单个IP、IP范围、CIDR格式或域名。
  
  **示例值：**
  - 单个IP: "192.168.1.1"
  - IP范围: "192.168.1.1-100"
  - CIDR: "192.168.1.0/24"
  - 域名: "example.com"
  
  **注意事项：**
  - 确保目标地址格式正确
  - 必需参数，不能为空
```

## 参数类型说明

### 布尔类型 (bool)

布尔类型参数有特殊处理：
- `true`: 只添加标志，不添加值（如 `--flag`）
- `false`: 不添加任何参数
- 支持多种输入格式：`true`/`false`、`1`/`0`、`"true"`/`"false"`

**示例：**
```yaml
- name: "verbose"
  type: "bool"
  description: "详细输出模式"
  required: false
  default: false
  flag: "-v"
  format: "flag"
```

### 字符串类型 (string)

最常用的参数类型，支持任意字符串值。

### 整数类型 (int/integer)

用于数值参数，如端口号、级别等。

**示例：**
```yaml
- name: "level"
  type: "int"
  description: "测试级别，范围1-5"
  required: false
  default: 3
  flag: "--level"
  format: "combined"  # --level=3
```

### 数组类型 (array)

数组会自动转换为逗号分隔的字符串。

**示例：**
```yaml
- name: "ports"
  type: "array"
  item_type: "number"
  description: "端口列表"
  required: false
  # 输入: [80, 443, 8080]
  # 输出: "80,443,8080"
```

## 示例

参考 `tools/` 目录下的现有工具配置文件：

- `nmap.yaml`: 网络扫描工具（包含 `scan_type` 和 `additional_args` 示例）
- `sqlmap.yaml`: SQL注入检测工具（包含 `additional_args` 示例）
- `nikto.yaml`: Web服务器扫描工具
- `dirb.yaml`: Web目录扫描工具
- `exec.yaml`: 系统命令执行工具

### 完整示例：nmap 工具配置

```yaml
name: "nmap"
command: "nmap"
args: ["-sT", "-sV", "-sC"]  # 默认扫描类型
enabled: true

short_description: "网络扫描工具，用于发现网络主机、开放端口和服务"

description: |
  网络映射和端口扫描工具，用于发现网络中的主机、服务和开放端口。
  
  **主要功能：**
  - 主机发现：检测网络中的活动主机
  - 端口扫描：识别目标主机上开放的端口
  - 服务识别：检测运行在端口上的服务类型和版本
  - 操作系统检测：识别目标主机的操作系统类型
  - 漏洞检测：使用NSE脚本检测常见漏洞

parameters:
  - name: "target"
    type: "string"
    description: "目标IP地址或域名"
    required: true
    position: 0
    format: "positional"
  
  - name: "ports"
    type: "string"
    description: "端口范围，例如: 1-1000"
    required: false
    flag: "-p"
    format: "flag"
  
  - name: "scan_type"
    type: "string"
    description: "扫描类型选项，例如: '-sV -sC'"
    required: false
    format: "positional"
  
  - name: "additional_args"
    type: "string"
    description: "额外的Nmap参数，例如: '--script vuln -O'"
    required: false
    format: "positional"
```

## 添加新工具

要添加新工具，只需在 `tools/` 目录下创建一个新的 YAML 文件，例如 `my_tool.yaml`：

```yaml
name: "my_tool"
command: "my-command"
args: ["--default-arg"]  # 固定参数（可选）
enabled: true

# 简短描述（推荐）- 用于工具列表，减少token消耗
short_description: "一句话说明工具用途"

# 详细描述 - 用于工具文档和AI理解
description: |
  工具详细描述，支持多行文本和Markdown格式。
  
  **主要功能：**
  - 功能1
  - 功能2
  
  **使用场景：**
  - 场景1
  - 场景2
  
  **注意事项：**
  - 使用时的注意事项
  - 权限要求
  - 性能影响

parameters:
  - name: "target"
    type: "string"
    description: |
      目标参数详细描述。
      
      **示例值：**
      - "value1"
      - "value2"
      
      **注意事项：**
      - 格式要求
      - 使用限制
    required: true
    position: 0  # 位置参数
    format: "positional"
  
  - name: "option"
    type: "string"
    description: "选项参数描述"
    required: false
    flag: "--option"
    format: "flag"
  
  - name: "verbose"
    type: "bool"
    description: "详细输出模式"
    required: false
    default: false
    flag: "-v"
    format: "flag"
  
  - name: "additional_args"
    type: "string"
    description: "额外的工具参数，多个参数用空格分隔"
    required: false
    format: "positional"
```

保存文件后，重启服务即可自动加载新工具。

### 工具配置最佳实践

1. **参数设计**
   - 将常用参数单独定义，便于AI理解和使用
   - 使用 `additional_args` 提供灵活性，支持高级用法
   - 为参数提供清晰的描述和示例

2. **描述优化**
   - 使用 `short_description` 减少token消耗
   - `description` 要详细，帮助AI理解工具用途
   - 使用Markdown格式增强可读性

3. **默认值设置**
   - 为常用参数设置合理的默认值
   - 布尔类型默认值通常设为 `false`
   - 数值类型根据工具特性设置

4. **参数验证**
   - 在描述中明确参数格式要求
   - 提供多个示例值
   - 说明参数的限制和注意事项

5. **安全性**
   - 对于危险操作，在描述中添加警告
   - 说明权限要求
   - 提醒仅在授权环境中使用

6. **单次执行时长与超时（最佳实践）**
   - 若某工具经常执行很久（如超过 10～30 分钟仍显示「执行中」），属于异常长时间挂起，建议：
     - 在 **config.yaml** 的 `agent.tool_timeout_minutes` 中设置单次工具最大执行时长（默认 10 分钟），超时后会自动终止并释放资源；
     - 需要更长扫描时再适当调大该值（如 20、30），不建议设为 0（不限制）；
     - 在任务监控页可对整条任务使用「停止任务」中断当前对话与后续工具调用；
     - 工具实现上尽量支持「可中断」或内置超时（如脚本内设 timeout），以便与系统超时协同。

## 禁用工具

要禁用某个工具，只需将配置文件中的 `enabled` 字段设置为 `false`，或者直接删除/重命名配置文件。

禁用后，工具不会出现在工具列表中，AI也无法调用该工具。

## 工具配置验证

系统在加载工具配置时会进行基本验证：

- ✅ 检查必需字段（`name`, `command`, `enabled`）
- ✅ 验证参数定义格式
- ✅ 检查参数类型是否支持

如果配置有误，系统会在启动日志中显示警告信息，但不会阻止服务器启动。错误的工具配置会被跳过，其他工具仍可正常使用。

## 常见问题

### Q: 如何传递多个参数值？

A: 对于数组类型参数，系统会自动转换为逗号分隔的字符串。对于需要传递多个独立参数的情况，可以使用 `additional_args` 参数。

### Q: 如何覆盖工具的默认参数？

A: 某些工具（如 `nmap`）支持 `scan_type` 参数来覆盖默认的扫描类型。对于其他情况，可以使用 `additional_args` 参数。

### Q: 工具执行超过 30 分钟一直显示「执行中」怎么办？

A: 属于异常长时间挂起，建议：
1. 在 **config.yaml** 中配置 `agent.tool_timeout_minutes`（默认 10），单次工具超过该分钟数会自动终止；
2. 在监控页对该任务使用「停止任务」立即中断；
3. 若该工具确实需要更长时间，可适当增大 `tool_timeout_minutes`，但不建议设为 0。

### Q: 工具执行失败怎么办？

A: 检查以下几点：
1. 工具是否已安装并在系统PATH中
2. 工具配置是否正确
3. 参数格式是否符合要求
4. 查看服务器日志获取详细错误信息

### Q: 如何测试工具配置？

A: 可以使用 `cmd/test-config/main.go` 工具测试配置加载：
```bash
go run cmd/test-config/main.go
```

### Q: 参数顺序如何控制？

A: 使用 `position` 字段控制位置参数的顺序。**位置 0 的参数（如 gobuster 的 `dir` 子命令）会紧跟在命令名后、所有标志参数之前**，以便兼容需要“子命令 + 选项”形式的 CLI。其余标志参数按在 `parameters` 列表中的顺序添加，再按 position 1、2… 添加其余位置参数。`additional_args` 会追加到命令末尾。

## 工具配置模板

### 基础工具模板

```yaml
name: "tool_name"
command: "command"
enabled: true

short_description: "简短描述（20-50字）"

description: |
  详细描述，说明工具的功能、使用场景和注意事项。

parameters:
  - name: "target"
    type: "string"
    description: "目标参数描述"
    required: true
    position: 0
    format: "positional"
  
  - name: "additional_args"
    type: "string"
    description: "额外的工具参数"
    required: false
    format: "positional"
```

### 带标志参数的工具模板

```yaml
name: "tool_name"
command: "command"
enabled: true

short_description: "简短描述"

description: |
  详细描述。

parameters:
  - name: "target"
    type: "string"
    description: "目标"
    required: true
    flag: "-t"
    format: "flag"
  
  - name: "option"
    type: "bool"
    description: "选项"
    required: false
    default: false
    flag: "--option"
    format: "flag"
  
  - name: "level"
    type: "int"
    description: "级别"
    required: false
    default: 3
    flag: "--level"
    format: "combined"
  
  - name: "additional_args"
    type: "string"
    description: "额外参数"
    required: false
    format: "positional"
```

## 相关文档

- 主项目 README: 查看 `README.md` 了解完整的项目文档
- 工具列表: 查看 `tools/` 目录下的所有工具配置文件
- API文档: 查看主 README 中的 API 接口说明

