#!/bin/bash

if [ "$#" -lt 2 ]; then
    echo "Usage: $0 name to_user1 [to_user2] ...(name: filename without suffix of yaml file)"
    exit 1
fi

# extract name and to_user from arguments
name="$1"
shift
to_users=("$@")

status_file="/tmp/bot_${name}.txt"

if [ -f "$status_file" ]; then
    last_status=$(cat $status_file)
else
    last_status="running"
fi

if pgrep -f "${name}.yml" &>/dev/null; then
    status="running"
    body="next notify would be sent when the bot is stopped"
else
    status="stopped"
    body="contact administrator please"
fi

echo "$status" > "$status_file"
if [ "$last_status" != "$status" ]; then
    for user in "${to_users[@]}"; do
        echo "$body"| mail -s "bot $status: $name" "$user"
    done
fi