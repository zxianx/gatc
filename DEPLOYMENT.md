# GATC 部署指南

##

生产环境需要挂配置文件

```
/opt/gatc/
├── conf/                    # 配置目录（从宿主机挂载）
│   ├── conf.yaml           # 应用配置
│   ├── resource.yaml       # 资源配置
│   ├── gcp/                # GCP 相关配置
│   │   ├── sa-key0.json    # 服务账户密钥
│   │   ├── gatc_rsa        # SSH 私钥
│   │   └── gatc_rsa.pub    # SSH 公钥
```

-v /opt/gatc/conf:/app/gatc/conf


打包配置
tar -czf ./output/config.tar.gz ./conf/gcp ./conf/*.yaml
到38 /opt/gatc  rz ， tar -xvf
sudo  chmod -R 777 ./conf
sudo docker compose -f docker-compose.prod.yml up -d
