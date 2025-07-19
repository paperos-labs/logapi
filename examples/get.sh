#!/bin/sh
set -e
set -u

. ./.env

echo ''
curl "${LOG_BASEURL}/api/logs/email_log/2025-07/123a.html" \
    --user "${LOG_USER}:${LOG_TOKEN}"
echo ''
curl "${LOG_BASEURL}/api/logs/email_log/2025-07/234.html" \
    --user "${LOG_USER}:${LOG_TOKEN}"
echo ''
curl "${LOG_BASEURL}/api/logs/email_log/2025-07/345.html" \
    --user "${LOG_USER}:${LOG_TOKEN}"
echo ''

echo ''
curl "${LOG_BASEURL}/api/logs/email_log/2024-07/123.html" \
    --user "${LOG_USER}:${LOG_TOKEN}"
echo ''
curl "${LOG_BASEURL}/api/logs/email_log/2024-07/234.html" \
    --user "${LOG_USER}:${LOG_TOKEN}"
echo ''
curl "${LOG_BASEURL}/api/logs/email_log/2024-07/345.html" \
    --user "${LOG_USER}:${LOG_TOKEN}"
echo ''
