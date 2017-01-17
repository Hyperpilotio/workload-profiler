#!/usr/bin/env bash
curl -XPOST localhost:7779/profilers --data-binary @nginx-profile.json
