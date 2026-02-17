# AI Tool Guide

通过 `POST /agent/process` 触发工具调用。

## 工具（所有工具仅支持数组，单修改传单元素，多修改传多元素，地址填写仅支持绝对路径）

- `view`：按行查看指定文件内容。
- `edit`：按行替换指定文件内容。（使用此工具前先查看文件）
- `shell`：执行 shell 命令。

## 调用格式

查看文件（`view`）：

```json
{
  "view": [{ "path": "", "start": 1, "end": 20 }]
}
```

编辑文件（`edit`）：

```json
{
  "edit": [
    {
      "path": "",
      "start": 1,
      "end": 1,
      "content": "替换文档第一行"
    }
  ]
}
```

执行 shell（`shell`）：

```json
{
  "shell": [
    {
      "command": "pwd",
      "cwd": "",
      "timeout_seconds": 20
    }
  ]
}
```
