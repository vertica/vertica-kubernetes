#!/bin/bash
set -e

start_cron(){
    # daemonizes, no need for &
    sudo /usr/sbin/crond
}

# Kubernetes start-up is a little weird
#  - in order to configure the host-list correctly, k8s
#    has to do an install_vertica, which writes to
#    non-volatile store
#  - but the agent needs things that will be created by
#    that install.
#  - so we don't start the agent until we find the database running 
start_agent_when_ready(){
    agent_started=No
    while [ $agent_started == No ]; do
        if [ -f /opt/vertica/config/admintools.conf ]; then
            # safe to try to run admintools
            db=$(/opt/vertica/bin/admintools -t show_active_db) || true
            case "$db"x in
                x)
                    sleep 15
                    ;;
                *)
                    echo "Starting vertica agent for db $db"
                    sudo /opt/vertica/sbin/vertica_agent start \
                         2> /tmp/agent_start.err \
                         1> /tmp/agent_start.out
                    echo "Agent started"
                    agent_started=Yes
                    ;;
            esac
        else
            sleep 15
        fi
    done 
}

restartNode(){
    if [ ! -f /opt/vertica/config/admintools.conf ]
    then
        echo "Vertica is not installed, expect manual user intervention for install.";
        sudo /usr/sbin/sshd -D
        # If we get here we fail to force restart of container:
        exit 1  
    fi
    # restart local Vertica node
    echo "Restart local node"
    /opt/vertica/sbin/python3 /opt/vertica/bin/re-ip-node.py --restart-node
    sudo /usr/sbin/sshd -D
}

reIpNode(){
    if [ ! -d /opt/vertica/config/licensing ] || [ -z $(ls -A /opt/vertica/config/licensing/*) ]
    then
        echo "Installing license..."
        mkdir -p /opt/vertica/config/licensing
        cp -r /home/dbadmin/licensing/ce/* /opt/vertica/config/licensing
    fi
    echo "Update IP address on local node"
    /opt/vertica/sbin/python3 /opt/vertica/bin/re-ip-node.py --re-ip-node
    exit $?
}

defaultEntrypoint(){
    echo "Vertica container is now running"
    sudo /usr/sbin/sshd -D
}

start_cron
start_agent_when_ready &

case $# in
    1) 
        case $1 in
            restart-vertica-node)
                restartNode
                ;;
            re-ip-vertica-node)
                reIpNode
                ;;
            *)
                echo "Invalid argument: $1"
                exit 1
                ;;
        esac
        ;;
    *)
        defaultEntrypoint
        ;;
esac


