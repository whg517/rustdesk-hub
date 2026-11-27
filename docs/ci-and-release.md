# 流水线与容器发布

| 触发 | 检查 | 产物 |
| --- | --- | --- |
| Pull request | `gofmt`、Oxlint、网页类型检查与构建、`go vet`、Go 竞态测试 | GitHub Actions 检查结果 |
| 推送到 `main` | 与 Pull request 一致 | Quay 镜像 `main` 和 `sha-<完整提交 SHA>` |
| 推送版本 tag | 校验 tag 后运行同一套检查 | 同名 Quay 镜像和含镜像 digest 的 GitHub Release |

镜像支持 `linux/amd64` 和 `linux/arm64`，内嵌管理网页，以 UID/GID `10001:10001` 运行。版本 tag 支持 `v1.2.3` 和 `v1.2.3-rc.1` 形式，后者会标记为预发布。GitHub Release 在版本镜像成功推送后创建，已有该版本 Release 时重跑会跳过发布。

## 配置 Quay

在 Quay 创建目标镜像仓库，给一个机器人账号授予该仓库的 `Write` 权限。在 GitHub 仓库 **Settings → Secrets and variables → Actions** 配置以下两项 Secret。

| Secret | 值 |
| --- | --- |
| `QUAY_USERNAME` | Quay 机器人账号全名，例如 `namespace+rustdesk_hub_ci` |
| `QUAY_TOKEN` | 该机器人的 token |

镜像默认推到 `quay.io/<GitHub owner>/<GitHub repo>`。使用其他 Quay 命名空间或镜像名称时，在同一设置页的 **Variables** 新增 `QUAY_IMAGE`，值形如 `quay.io/namespace/rustdesk-hub`。两项 Secret 都未设置时，`main` 流水线仍会构建镜像，并在运行摘要中提示推送待配置。版本发布需要两项 Secret。

## 发布版本

检查 `main` 上待发布的提交。下面使用 `v1.2.3` 举例。

```sh
git switch main
git pull --ff-only
git tag v1.2.3
git push origin refs/tags/v1.2.3
```

Release 页面列出按 digest 拉取的命令。使用私有镜像仓库时，部署机须先执行 `docker login quay.io` 并使用具备读取权限的账号。

## 运行镜像

从项目根目录复制 `config.example.yaml` 为 `config.yaml`，使用 HTTPS 反向代理时设置以下字段，并使容器用户可读该文件。

```yaml
listen: "0.0.0.0:8080"
public-url: "https://hub.example.com"
behind-proxy: true
```

保留示例中的数据库路径 `data/hub.sqlite3`。新实例先初始化管理员，再启动服务。在以下命令中使用 Release 页的实际镜像引用。

```sh
image=quay.io/namespace/rustdesk-hub:v1.2.3

docker volume create rustdesk-hub-data
docker run --rm -it \
    --mount type=volume,source=rustdesk-hub-data,target=/app/data \
    --mount "type=bind,source=$PWD/config.yaml,target=/app/config.yaml,readonly" \
    "$image" init-owner owner

docker run -d --name rustdesk-hub --restart unless-stopped \
    --mount type=volume,source=rustdesk-hub-data,target=/app/data \
    --mount "type=bind,source=$PWD/config.yaml,target=/app/config.yaml,readonly" \
    -p 127.0.0.1:8080:8080 \
    "$image"
```

将同机 HTTPS 反向代理指向 `127.0.0.1:8080`，保留请求方法、`Authorization` 和请求体。启动后运行 `curl -fsS http://127.0.0.1:8080/healthz` 检查 SQLite 是否就绪。升级时备份数据库，使用同一个数据卷启动新版本。
