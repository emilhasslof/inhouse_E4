#!/bin/bash
# Suppress a harmless protobuf double-registration warning from go-steam internals.
# See: https://protobuf.dev/reference/go/faq#namespace-conflict
GOLANG_PROTOBUF_REGISTRATION_CONFLICT=warn go run .
