# ClipBridge 部署

## 首次部署

1. 为服务器创建 A/AAAA 记录，并等待域名解析到 VPS。
2. 安装 Docker Engine 与 Compose 插件，复制仓库到 `/opt/clipbridge`。
3. `cp .env.example .env`，以 `openssl rand -base64 48` 生成 JWT Secret。
4. 从仓库根目录运行 `./scripts/setup-server.sh`。
5. 先启动 HTTP 内网服务：`docker compose up -d`。
6. 使用 Certbot webroot/standalone 首次签发证书后运行
   `docker compose --profile tls up -d`。

示例签发（先确保 80 端口可从公网访问，且暂时没有 Nginx 占用）：

```bash
sudo certbot certonly --standalone -d clipbridge.example.com
docker compose --profile tls up -d
sudo install -m 755 certbot/renewal-pre.sh /etc/letsencrypt/renewal-hooks/pre/clipbridge-nginx
sudo install -m 755 certbot/renewal-post.sh /etc/letsencrypt/renewal-hooks/post/clipbridge-nginx
sudo certbot renew --dry-run
```

续期前置钩子只会短暂停止 Nginx 以释放 80 端口，中继容器不会停止；后置钩子会恢复
Nginx。Certbot 的 systemd timer 负责定期检查证书。

不建议为裸 IP 部署正式服务；Let’s Encrypt 的常规域名证书流程要求可验证的域名。

## 防火墙

仅开放 SSH 实际端口、TCP 80 和 TCP 443。8080 绑定到 `127.0.0.1`，不得对公网开放。
Ubuntu 可使用：

```bash
sudo ufw default deny incoming
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw allow <实际SSH端口>/tcp
sudo ufw enable
```

在确认新 SSH 规则可用前不要关闭现有 SSH 会话。

## 备份与恢复

SQLite 使用 WAL。优先在 relay 容器停止后打包 `deployment/data/`，或使用
`sqlite3 clipbridge.db ".backup backup.db"` 获取一致副本。备份应加密并限制权限。恢复前停止
服务、备份当前目录，再用同版本镜像启动并检查 `/ready`。

## 更新与回滚

```bash
git fetch --tags
git checkout <审核过的版本>
docker compose build --pull relay
docker compose up -d
```

更新前备份数据库并记录当前 Git 提交和镜像摘要。回滚时检出旧提交、恢复兼容的数据库备份，
重新构建并检查健康状态。迁移只向前执行；涉及破坏性 schema 变化的版本必须在发行说明中
提供独立回滚迁移。

Docker 已对 JSON 日志启用大小和数量限制。若改为宿主文件日志，可安装
`logrotate/clipbridge`。
