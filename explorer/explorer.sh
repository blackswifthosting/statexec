#!/bin/bash

CURDIR=$(dirname $0)
[ "$CURDIR" = "." ] && CURDIR=$(pwd)

VMSERVER=${VMSERVER:-http://localhost:8428}
DOCKERCOMPOSE_CMD=$(docker-compose version >/dev/null 2>&1 && echo "docker-compose" || echo "docker compose")

DASHBOARDURL="http://localhost:3000/d/statexec/statexec-dashboard"

function waitForVM {
    echo "Waiting for VictoriaMetrics to start"
    while ! curl -s ${VMSERVER}/health -o /dev/null; do
        echo -n "."
        sleep 1
    done
    echo
}

function waitForGrafana {
    echo "Waiting for Grafana to start"
    while ! curl -s http://localhost:3000/api/health -o /dev/null; do
        echo -n "."
        sleep 1
    done
    echo
}

# Import Prometheus exposition format
function importPromFile {
    import=$1
    [ "$import" = "" ] && import=$CURDIR/import

    waitForVM
    waitForGrafana

    tmpfile=$(mktemp)
    find $import -type f -name "*.prom" -print0 | while IFS= read -r -d '' file; do
        echo "Importing $file"

        # Prometheus metrics
        curl -X POST ${VMSERVER}/api/v1/import/prometheus -T "$file" \
            || { echo "Cannot import $file" ;  exit 1; }

        # Grafana annotations
        grep "^#grafana-annotation" $file | while IFS= read -r line; do
            # Format: #grafana-annotation <annotation>
            annotation=$(echo $line | sed 's/^#grafana-annotation //')
            curl -so /dev/null -X POST -H "Content-Type: application/json" -d "${annotation}" http://localhost:3000/api/annotations \
                || { echo "Cannot create grafana annotations from $file" ;  exit 1; }
        done

        startTime=$(grep "^statexec_metric_collect_duration_ms" $file | sed -e 's/.*\} .* //' | head -n 1)
        endTime=$(grep "^statexec_metric_collect_duration_ms" $file | sed -e 's/.*\} .* //' | tail -n 1)
        echo -e "${startTime}\n${endTime}" >> $tmpfile
    done

    minStartTime=$(cat $tmpfile | sort -n | head -n 1)
    maxEndTime=$(cat $tmpfile | sort -n | tail -n 1)
    rm $tmpfile
    
    maxEndTime=$(( ${maxEndTime} + 1000 ))

    open "${DASHBOARDURL}?orgId=1&from=${minStartTime}&to=${maxEndTime}"
}

function start {
    (cd $CURDIR && $DOCKERCOMPOSE_CMD up -d)
}

function stop {
    (cd $CURDIR && $DOCKERCOMPOSE_CMD down)
}

# See https://docs.victoriametrics.com/Single-server-VictoriaMetrics.html#how-to-import-data-in-prometheus-exposition-format

case $1 in 
    stop)
        stop
        ;;
    start)
        start
        ;;
    restart)
        stop
        start
        ;;
    explore)
        stop
        start
        importPromFile ${@:2}
        ;;
    import)
        importPromFile ${@:2}
        ;;
    open)
        open "${DASHBOARDURL}"
        ;;
    *)
        echo usage && exit
        ;;
esac
