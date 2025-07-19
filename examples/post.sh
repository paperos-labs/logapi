#!/bin/sh
set -e
set -u

. ./.env

curl "${LOG_BASEURL}/api/logs" \
    --user "${LOG_USER}:${LOG_TOKEN}" \
    -H "X-File-Date: 2025-07" \
    -H "X-File-Name: 123a.html" \
    -H "Content-Type: text/html" \
    --data-binary '<html><h1>Hello, World!</h1></html>'
