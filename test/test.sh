#!/usr/bin/env bash
curl -XPOST localhost:7779/profilers --data-binary @config.json
