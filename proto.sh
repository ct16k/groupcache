#! /bin/sh

# Make sure the script fails fast.
set -e
set -u
set -x

PROTO_DIR=testpb

protoc -I=$PROTO_DIR \
    --go_out=$PROTO_DIR \
    --go_opt=paths=source_relative \
    --fastmarshal_out=paths=source_relative:$PROTO_DIR \
    --fastmarshal_opt=apiversion=v2,enableunsafedecode=true \
    $PROTO_DIR/test.proto

PROTO_DIR=groupcachepb

protoc -I=$PROTO_DIR \
    --go_out=$PROTO_DIR \
    --go_opt=paths=source_relative \
    --fastmarshal_out=paths=source_relative:$PROTO_DIR \
    --fastmarshal_opt=apiversion=v2,enableunsafedecode=true \
    $PROTO_DIR/groupcache.proto

protoc -I=$PROTO_DIR \
   --go_out=. \
   --go_opt=paths=source_relative \
   --fastmarshal_out=paths=source_relative:. \
   --fastmarshal_opt=apiversion=v2,enableunsafedecode=true \
    $PROTO_DIR/example.proto
