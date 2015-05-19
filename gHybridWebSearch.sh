#!/bin/bash

## gHybridWebSearch version 0.2 - Quick Hybrid Web Search for Old, Backup and Unreferenced ##
## Files for Sensitive Information based on a custom dictionary (c)gfragkos 2012          ##
## You may modify, reuse and distribute the code freely as long as it is referenced back   ##
## to the author using the following line: ..based on gHybridWebSearch by @drgfragkos      ##

if [ "$1" == "" ]; then
echo -ne "You need to pass a URL as an argument to work.\n  Usage: ./${0##*/} www.example.com\n"
exit
fi

echo -ne "Script: $0\tURL: $1\n"
server=$1
port=80
counter=0

while read line; do
sleep 0.10
counter=`expr $counter + 1`
echo -ne "$line\t\t\t"
echo -e "GET /$line HTTP/1.0\nHost: $server\n" | netcat $server $port | head -1

done < "hybridWebSearch.dic" | tee .log.dat

cat .log.dat | grep "200 OK" > output-200.txt
sleep 0.10
cat .log.dat | grep -v "404 Not Found" > output-ex404.txt

#rm .log.dat   ## in case the main log file in not needed to be kept for further searches



