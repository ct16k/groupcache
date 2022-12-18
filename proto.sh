#! /bin/sh

# Make sure the script fails fast.
set -e
set -u
set -x

PROTO_DIR=testpb

protoc -I=$PROTO_DIR \
    --go_out=$PROTO_DIR \
    $PROTO_DIR/test.proto

PROTO_DIR=groupcachepb

protoc -I=$PROTO_DIR \
    --go_out=$PROTO_DIR \
    $PROTO_DIR/groupcache.proto

protoc -I=$PROTO_DIR \
   --go_out=. \
    $PROTO_DIR/example.proto
