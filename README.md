# logapi

An API to store, retrieve, and automatically archive log files.

```sh
/mnt/storage/blobs/api_log/2025-07/1234.json
/mnt/storage/blobs/api_log/2025-07.tar.zst#2025-07/1234.json
```

# Table of Contents

- Usage
- Build
- Deploy
- API Keys

# Usage

### `POST /api/logs`

```sh
curl -X POST "${LOG_BASEURL}/api/logs" \
    --user "${LOG_USER}:${LOG_TOKEN}" \
    -H "X-File-Date: 2025-07" \
    -H "X-File-Name: 1234.json" \
    -H "Content-Type: application/json" \
    --data-binary '{ "foo": "bar" }'
```

### `GET /api/logs/<user>`

```sh
curl "${LOG_BASEURL}/api/logs/${LOG_USER}" \
    --user "${LOG_USER}:${LOG_TOKEN}"
```

```json
{ "results": ["2025-07"] }
```

### `GET /api/logs/<user>/<YYYY-MM>`

```sh
curl "${LOG_BASEURL}/api/logs/${LOG_USER}/2025-07" \
    --user "${LOG_USER}:${LOG_TOKEN}"
```

```json
{ "results": ["1234.json"] }
```

### `GET /api/logs/<user>/<YYYY-MM>/<filename>`

```sh
curl "${LOG_BASEURL}/api/logs/${LOG_USER}/2025-07/1234.json" \
    --user "${LOG_USER}:${LOG_TOKEN}"
```

# Build

```sh
GOOS=linux GOARCH=amd64 GOAMD=v2 go build -o ./logapid-linux-x86_64 ./cmd/logapid/

scp -rp ./logapid-linux-x86_64 server:~/bin/logapid
```

# Deploy

```sh
webi serviceman
serviceman add --name logapid -- \
    logapid --tsv ~/.config/logapid/credentials.tsv --storage /mnt/storage/blobs --port 8080
```

# Set API Keys

```sh
go run ./cmd/csvpass/ set --algorithm=plain 'api_log'
go run ./cmd/csvpass/ set --algorithm=pbkdf2,4096,16,SHA-256 'api_log'
go run ./cmd/csvpass/ set --algorithm=bcrypt,10 'webhooks_log'
```
