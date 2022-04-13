#!/bin/sh

echo "Clearing out public ssh keys"

rm -f /root/.ssh/authorized_keys
rm -f /home/ubuntu/.ssh/authorized_keys
