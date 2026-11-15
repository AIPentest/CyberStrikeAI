# 引用与外链（示例）

本文件用于验证技能包内 **`references/`** 目录是否被列表 API、HTTP `resource_path` 及多代理本机文件工具正常识别。

## 测试方式（授权环境内）

1. `GET /api/skills/cyberstrike-pi-demo` 响应中的 `package_files` 应包含 `references/citations.md`。
2. `GET /api/skills/cyberstrike-pi-demo?resource_path=references/citations.md` 应返回本文内容。
3. 多代理且开启 `skills.filesystem_tools` 时，可通过相对路径读取本文件。

## 占位引用

- [OWASP Testing Guide](https://owasp.org/www-project-web-security-testing-guide/)（仅作链接格式示例）
