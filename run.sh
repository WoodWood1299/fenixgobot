#!/bin/bash
set -e

docker run -d -v .store:/.store --name fenixgobot --env-file .env --restart unless-stopped fenixgobot:alpha