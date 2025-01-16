#!/bin/sh
USERNAME=test PASSWORD=1234 go run `ls *.go | grep -v _test.go`