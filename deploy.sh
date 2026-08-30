#!/usr/bin/env bash
set -euo pipefail

FB="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROD="$HOME/Library/Application Support/FocalboardMy"
LABEL="local.focalboard.server"

NODE_V="$(node --version 2>/dev/null || echo none)"
case "$NODE_V" in
  v20.11.*) ;;
  *) echo "Нужен Node 20.11, сейчас $NODE_V. Выполните: cd webapp && nvm use"; exit 1 ;;
esac

echo "==> сборка сервера"
make -C "$FB" server
echo "==> сборка фронтенда"
make -C "$FB" webapp

echo "==> остановка службы"
launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true

echo "==> копирование артефактов"
rsync -a --delete "$FB/webapp/pack/" "$PROD/pack/"
cp "$FB/bin/focalboard-server" "$PROD/focalboard-server"

echo "==> запуск службы"
launchctl bootstrap "gui/$(id -u)" "$HOME/Library/LaunchAgents/$LABEL.plist"
sleep 5

if curl -fsS -H "Authorization: Bearer $(cat "$HOME/.focalboard-token")" -H "X-Requested-With: XMLHttpRequest" http://localhost:8088/api/v2/teams/0/boards >/dev/null; then
  echo "OK: сервис отвечает на 8088"
else
  echo "ОШИБКА, последние строки лога:"
  tail -20 /tmp/focalboard.err
  exit 1
fi
