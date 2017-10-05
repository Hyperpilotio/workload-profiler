#!/usr/bin/env bash

backup_file_path="/tmp/influx/backups"
bucket="influx_backup"

# read the options
if [[ -z "$1" ]]; then
    echo "please see 'help'"
    exit 1
fi
BACKUP=`getopt -q -l host:,port:,backup-host:,name:,influx-username::,influx-password::  -- "$@"`
RESTORE=`getopt -q -l name:,no-cache, -- "$@"`
if [[ "$1" == "backup" ]]; then
    OPERATOR=$1
    eval set -- "$BACKUP"
elif [[ "$1" == "restore" ]]; then
    OPERATOR=$1
    eval set -- "$RESTORE"
elif [[ "$1" == "house-keeping" ]]; then
    rm -rf $backup_file_path
    echo "cache are all cleared."
    echo "bye!"
    exit 0
elif [[ "$1" == "help" ]]; then
    printf "[hyperpilot_influx tool]
    Backup whole influxDB to AWS S3 bucket and restore
    Usage:
        hyperpilot_influx backup <options>
        hyperpilot_influx restore <options>
        hyperpilot_influx house-keeping: clean all local cached snapshot files
    options:
        --host: influxDB host url (only backup operation needed)
        --port: influxDB server port (only backup operation needed)
        --backup-host: influxDB_backup_host:port (only backup operation needed)
        --name: backup / restore file key name
        --influx-username(optional): influxdb user, default is set to 'root' (only backup operation needed)
        --influx-password(optional): influxdb password, default is set to 'default' (only backup operation needed)
        --no-cache(optinal): use local copy of snapshot if this flag is not provided, else it will pull from S3 \n"
    exit 1
else
    echo "must be backup / restore"
    exit 1
fi

# extract options and their arguments into variables.
while true ; do
    case "$1" in
        --host )
            case "$2" in
                "" ) echo "flag $1 contains no value" ; exit 1 ;;
                *  ) HOST=$2; shift 2 ;;
            esac ;;
        --port )
            case "$2" in
                "" ) echo "flag $1 contains no value" ; exit 1 ;;
                *  ) PORT=$2; shift 2 ;;
            esac ;;
        --backup-host )
            case "$2" in
                "" ) echo "flag $1 contains no value" ; exit 1 ;;
                *  ) BACKUP_HOST=$2; shift 2 ;;
            esac ;;
        --name )
            case "$2" in
                "" ) echo "flag $1 contains no value" ; exit 1 ;;
                *  ) NAME=$2; shift 2 ;;
            esac ;;
        --influx-username )
            case "$2" in
                "" ) echo "flag $1 show but contains no value" ; exit 1 ;;
                *  ) INFLUX_USERNAME=$2 ; shift 2 ;;
            esac ;;
        --influx-password )
            case "$2" in
                "" ) echo "flag $1 show but contains no values" ; exit 1 ;;
                *  ) INFLUX_PASSWORD=$2 ; shift 2 ;;
            esac ;;
        --no-cache ) NO_CACHE=true ; shift 1 ;;
        -- ) shift ; break ;;
        *  ) echo "error parameter: $1" ; exit 1 ;;
    esac
done

if [[ -z "$PORT" ]]; then
    PORT=8086
fi

# default username set to 'root'
if [[ -z "$INFLUX_USERNAME" ]]; then
    INFLUX_USERNAME="root"
fi

# default password set to 'default'
if [[ -z "$INFLUX_PASSWORD" ]]; then
    INFLUX_PASSWORD="default"
fi


echo "OPERATION: $OPERATOR"
echo "HOST = $HOST"
echo "NAME = $NAME"
echo "INFLUX_USERNAME = $INFLUX_USERNAME"
echo "INFLUX_PASSWORD = $INFLUX_PASSWORD"


file="$NAME.tar.gz"

case "$OPERATOR" in
    backup  )
        mkdir -p $backup_file_path
        # backup metastore
        influxd backup -host $BACKUP_HOST $backup_file_path

        # search for databases
        dbs=($(influx -host $HOST -port $PORT -username $INFLUX_USERNAME -password $INFLUX_PASSWORD -execute 'show databases' -format json | jq -c '.results[0].series[0].values[] | join([])'))
        # backup databases

        for db in "${dbs[@]}"; do
            normalized_db_name="${db#\"}"
            normalized_db_name="${normalized_db_name%\"}"
            echo "backing up $normalized_db_name"
            echo "influxd backup -host $BACKUP_HOST -database $normalized_db_name $backup_file_path/$normalized_db_name"
            influxd backup -host $BACKUP_HOST -database $normalized_db_name $backup_file_path/$normalized_db_name
        done
        # tar whole directory
        tar zcvf "$NAME.tar.gz" -C $backup_file_path .

        # upload to s3
        # create bucket (this will auto check if this bucket exists)
        echo "create s3 bucket"
        aws s3api create-bucket --bucket $bucket --region us-east-1

        # upload tar file
        echo "upload file"
        echo "aws cp $file s3://$bucket/$NAME.tar.gz"
        aws s3 cp $file s3://$bucket/$file

        printf "influxDB backup successfully
backup name: $NAME
you can run ./hyperpilot_influx.sh restore command to restore whole database
bye!\n
"
        ;;
    restore )

        # download file from s3 by specified name
        if [[ "$NO_CACHE" == "true" || ! -f $backup_file_path/$file ]]; then
            aws s3 cp s3://$bucket/$NAME.tar.gz $backup_file_path/$file
        fi
        # untar zip file
        mkdir -p $backup_file_path/cache/$NAME
        tar zxvf $backup_file_path/$NAME.tar.gz -C $backup_file_path/cache/$NAME

        sys_info=$(influx -execute 'show diagnostics' -format json)
        # detect data dir
        idx=$(echo $sys_info | jq '.results[0].series[] | select (.name=="config-data")' | jq '.columns | index("dir")')
        DATA_DIR=$(echo $sys_info | jq '.results[0].series[] | select (.name=="config-data")' | jq --arg idx "$idx" '.values[0][$idx | tonumber]')

        # detect meta dir
        idx=$(echo $sys_info | jq '.results[0].series[] | select (.name=="config-meta")' | jq '.columns | index("dir")')
        META_DIR=$(echo $sys_info | jq '.results[0].series[] | select (.name=="config-meta")' | jq --arg idx "$idx" '.values[0][$idx | tonumber]')

        META_DIR=${META_DIR%\"}
        META_DIR=${META_DIR#\"}
        DATA_DIR=${DATA_DIR%\"}
        DATA_DIR=${DATA_DIR#\"}
        echo "meta dir: $META_DIR"
        echo "data dir: $DATA_DIR"

        ## start restoring
        # restore meta
        influxd restore -metadir $META_DIR $backup_file_path/cache/$NAME
        # restore database
        files=($(ls $backup_file_path/cache/$NAME))
        for db in "${files[@]}"; do
            echo "restoring database $db"
            # restore database
            if ls $backup_file_path/cache/$NAME/$db/$db* 1> /dev/null 2>&1; then
                echo "restoring data"
                influxd restore -database $db -datadir $DATA_DIR $backup_file_path/cache/$NAME/$db
            else
                echo "empty database $db"
            fi
        done

        chown -R influx:influx $META_DIR
        chown -R influx:influx $DATA_DIR

        # kill process

        process=$(ps aux | grep influxdb)
        echo $process
        pid=$(echo $process | grep bin/influxd | awk '{print $2}')
        echo "killing Influxdb process: $pid"
        kill -9 $pid

        # update influx deployment
        printf "ok, done.\n please restart your influxd. \nbye!\n"
esac

rm -rf $file
