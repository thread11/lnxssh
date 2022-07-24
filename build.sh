#!/bin/bash

set -e

printf "\n-- $(date) -- Starting --\n"

[ -d build ] && rm -rf build
mkdir -p build

cp -a lnxssh.go build/
cp -ar template build/
cp -ar static build/

printf "\n-- $(date) -- Building --\n"

# apt-get install gcc glibc-static
# yum install gcc glibc-static
go build -ldflags="-s -w -linkmode=external -extldflags=-static" -o build/lnxssh build/lnxssh.go

[ -f build/lnxssh.go ] && rm build/lnxssh.go

tar czf lnxssh.tar.gz build --transform s/^build/lnxssh/

printf "\n-- $(date) -- Finished --\n"
