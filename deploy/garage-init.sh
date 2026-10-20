#!/bin/sh
# One-time Garage cluster layout: Garage boots healthy but stores nothing until a
# layout is assigned. Run once after the stack is up.
#
# Override COMPOSE to match how you run the stack, e.g. from a source checkout:
#   COMPOSE="docker compose -f deploy/docker-compose.yml --env-file .env" deploy/garage-init.sh
set -eu

COMPOSE=${COMPOSE:-docker compose}

NODE_ID=$($COMPOSE exec -T garage /garage node id -q 2>/dev/null | head -n1 | cut -d@ -f1)
if [ -z "$NODE_ID" ]; then
	echo "Could not read Garage node id — is the garage service up?" >&2
	exit 1
fi

echo "Assigning layout to node $NODE_ID"
$COMPOSE exec -T garage /garage layout assign "$NODE_ID" -z dc1 -c 1G
$COMPOSE exec -T garage /garage layout apply --version 1

echo "Garage layout applied. Create the attachments bucket + an app key with:"
echo "  $COMPOSE exec garage /garage bucket create attachments"
echo "  $COMPOSE exec garage /garage key create lookupkb-app   # paste keys into .env"
