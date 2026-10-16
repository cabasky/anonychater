# anonychater

匿名聊天室，支持图片、引用回复、换马甲、复读 +1。

## 构建

```bash
protoc --go_out=. --go_opt=paths=source_relative proto/chat.proto
sqlc generate
go build -o anonychater .
```

## 运行

```bash
./anonychater                    # 默认 :8080
./anonychater -port=9000
./anonychater -uds=/tmp/anonychater.sock
./anonychater -v                 # 调试日志
```

## 部署

单二进制，无运行时依赖。