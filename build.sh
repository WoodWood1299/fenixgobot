#!/bin/bash
set -e

docker buildx build --rm -t fenixgobot:alpha ./src
