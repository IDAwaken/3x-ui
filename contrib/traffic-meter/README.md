# 可选的逐目标流量计量组件

该组件为面板的“系统历史 → 流量”提供逐目标字节数据。面板自身只查询统计库，不根据访问次数估算流量，也不会自动修改代理出站。启用前需要由管理员单独部署计量来源、采集器与本机转发配置。

## 组件边界

固定使用 GOST 3.3.3 与 `go-gost/x v0.28.1`，附带显式启用的逐目标计量补丁。它是计量定制构建，不能当作未修改的官方包：TCP 排除 SOCKS 协商，UDP 按实际目标有效载荷计数；共享回包地址无法区分域名时保留地址端口。没有网页正文记录、请求头抓取或 TLS 解密。

活动连接每三十秒报告累计值，结束时报告最终累计。`traffic_ingest.py` 每分钟批量入 SQLite，以会话序号、累计值和文件位置去重。小时聚合保存九十天，原始报告有限轮转。异常退出可能丢失尚未报告的数据，该统计不作为运营商计费账单。

## 部署文件与路径

- `gost-target-stats.patch`：相对固定依赖版本的计量补丁。
- `gost.example.json`：仅监听 `127.0.0.1:13080` 的配置示例。
- `gost-meter.service`：独立的计量用户及本机转发进程。
- `traffic_ingest.py`、`test_traffic_ingest.py`：采集器与重复、乱序、丢失报告、文件续读的验证。
- `3x-ui-sites.service`、`3x-ui-sites.timer`：每分钟入库示例。
- `x-ui-meter.conf`：面板启动顺序示例，不随面板安装自动启用。

服务示例假定管理员已创建 `gost-meter` 用户，将程序放在 `/usr/local/gost-meter/gost`，配置放在 `/etc/gost-meter/config.json`，原始报告写入 `/var/lib/3x-ui-meter`。配置只允许管理员与计量用户读取，原始目录权限设为 `700`。采集器安装为 `/usr/local/libexec/3x-ui-traffic-ingest`，由受限任务写入 `/var/lib/3x-ui-sites/traffic.db`，数据库权限为 `600`。

管理员需单独评估并配置代理出站经该 SOCKS 服务转发。修改出站会改变代理运行路径，应先完成隔离验证和回滚准备。这里不包含任何个人服务器配置、节点认证或生产部署脚本。

## 固定版本重建

在固定的 `go-gost/x v0.28.1` 源码目录应用补丁，然后在 GOST 3.3.3 主项目中指向补丁后的依赖源码：

```bash
patch -p1 < /补丁路径/gost-target-stats.patch
# 切换到主项目目录，再配置依赖替换并编译
go mod edit -replace=github.com/go-gost/x=../source-x
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags='-w -s -X main.version=3.3.3-meter1' -o gost-meter ./cmd/gost
```

正式配置中的 `recorder.targetStats` 必须为 `true`，不启用嗅探。未应用补丁的官方记录口径不能直接替代此计量来源。升级时需重新适配并验证固定字节短连接、长连接、多目标 UDP、正常重启末尾计数和数据库恢复。

## 初始化与查询

先执行采集器初始化表，并在首次正式启用全量计量时写入元数据。以下初始化只应用于新部署，已有统计应保留原始启用时间：

```bash
/usr/local/libexec/3x-ui-traffic-ingest
python3 - <<'INIT'
import sqlite3
with sqlite3.connect('/var/lib/3x-ui-sites/traffic.db') as db:
    db.execute("INSERT OR IGNORE INTO metadata SELECT 'full_started',value FROM metadata WHERE key='started'")
INIT
```

`full_started` 存在时，面板只读 `target_hourly` 作为全量来源；没有该标记时兼容旧 `hourly` 分类库，二者不能混为同口径历史。不能补回未采集的过去数据。

接口复用面板认证，按上传与下载合计排序，支持 `?site=` 精确查询单个域名或地址端口。域名跨端口合并，未知地址保留端口；超过一百个目标时单站趋势按需加载。

## 测试与备份

```bash
python3 -m unittest discover -s contrib/traffic-meter -p 'test_*.py'
```

定期使用 SQLite 在线备份保存整个统计库，包含会话和文件检查点。恢复时先停止采集，保留当前库，再恢复整库并重放仍保留的原始报告；已删除的报告无法补回。不得单独清空去重表强行重放。
