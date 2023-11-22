# pandora-next-license-manager

## 动态 ip 自动更改 license.jwt

读取/data 目录下的端口，在 IPv4 或 IPv6 任一 IP 变动后重新获取 license 并热重载

### 使用方法

修改 main.go 中的 Key

> curl -fLO "https://dash.pandoranext.com/data/\<\<KEY>>/license.jwt"

将编译文件复制到容器 /opt/app

修改 entrypoint.sh 为如下内容

```shell
#!/bin/sh

chmod +x /opt/app/daemon
/opt/app/daemon
```
