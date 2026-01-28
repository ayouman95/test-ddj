#!/bin/bash

# Must run as root
if [ "$EUID" -ne 0 ]; then 
  echo "Please run as root"
  exit 1
fi

echo "Applying kernel tuning for high concurrency..."

# 1. Increase system-wide file descriptor limit
sysctl -w fs.file-max=1000000

# 2. Expand ephemeral port range (default is usually ~28k ports, expand to ~64k)
sysctl -w net.ipv4.ip_local_port_range="1024 65535"

# 3. Increase backlog queues to prevent drops during ramp-up/spikes
sysctl -w net.core.somaxconn=65535
sysctl -w net.ipv4.tcp_max_syn_backlog=65535

# 4. Enable TIME_WAIT reuse (Critical for frequent short connections on same ports)
sysctl -w net.ipv4.tcp_tw_reuse=1

# 5. Faster recycling of FIN_WAIT sockets
sysctl -w net.ipv4.tcp_fin_timeout=15

echo "------------------------------------------------"
echo "Kernel parameters updated."
echo "NOTE: These changes are ephemeral. Add them to /etc/sysctl.conf to persist."
echo "------------------------------------------------"
echo "Check your current user ulimit with: ulimit -n"
echo "You may need to edit /etc/security/limits.conf to raise hard limits for your user:"
echo "* soft nofile 65535"
echo "* hard nofile 65535"
echo "------------------------------------------------"
