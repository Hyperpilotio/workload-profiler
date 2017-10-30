#!/usr/bin/env bash

backup_file_path="/tmp/influx/backups"
bucket="hyperpilot_influx_backup"

# read the options
if [[ -z "$1" ]]; then
    echo "please see 'help with -?"
    exit 1
fi

# extract options and their arguments into variables.
while getopts ":h::b::n:u::p::o:a:d:k:?c" args ; do
    case $args in
        \?)
            printf "[hyperpilot_influx tool]
Backup whole influxDB to AWS S3 bucket and restore
Usage:
    hyperpilot_influx -o backup <options>
    hyperpilot_influx -o restore <options>
    hyperpilot_influx -o house-keeping: clean all local cached snapshot files
Example:
     ./hyperpilot_influx.sh -o backup -h 35.185.234.32 -b 35.185.234.32:8088 -n tech-demo -p=8086
options:
    -o: operation: backup / restore /house-keeping
    -h: influxDB host url with port (only backup operation needed)
    -b: influxDB_backup_host:port (only backup operation needed)
    -d: specify which database needed to backup, if this option set, -h value won't needed
    -n: backup / restore file key name
    -u(optional): influxdb user, default is set to 'root' (only backup operation needed)
    -p(optional): influxdb password, default is set to 'default' (only backup operation needed)
    -a(optional): aws s3 bucket name, default is set to hyperpilot_influx_backup
    -k(optional): influxdb service name in kubernetes, it will search endpoints of influxdb by using kubectl so you need to set KUBECONFIG in env to use this flag
    -c(optional): use local copy of snapshot if this flag is not provided, else it will pull from S3 \n"
            exit 1
            ;;
        a)
            bucket=${OPTARG}
            ;;
        o)
            case ${OPTARG} in
                backup)
                    OPERATION=${OPTARG}
                    ;;
                restore)
                    OPERATION=${OPTARG}
                    ;;
                house-keeping)
                    rm -rf $backup_file_path
                    echo "cache are all cleared."
                    echo "bye!"
                    exit 0
                    ;;
                *)
                    echo "wrong operation: ${OPTARG}"
                    echo "bye!"
                    exit 0
                    ;;
            esac
            ;;
        h)
            str_host=$(echo ${OPTARG} | awk -F: '{print $1}')
            if [[ -z $str_host ]]; then
                HOST=localhost
            else
                HOST=$str_host
            fi
            str_port=$(echo ${OPTARG} | awk -F: '{print $2}')
            if [[ -z $str_port ]]; then
                PORT=8086
            else
                PORT=$str_port
            fi
            ;;
        b)
            BACKUP_HOST=${OPTARG}
            ;;
        n)
            NAME=${OPTARG}
            ;;
        u)
            INFLUX_USERNAME=${OPTARG}
            ;;
        p)
            INFLUX_PASSWORD=${OPTARG}
            ;;
        c)
            NO_CACHE=true
            ;;
        d)
            DATABASE=${OPTARG}
            ;;
        k)
            KUBE_INFLUX=${OPTARG}
            ;;
    esac
done

if [[ ! -z "$KUBE_INFLUX" ]]; then
    #statements
    if [[ -z "$KUBECONFIG" ]]; then
        #statements
        echo "please set KUBECONFIG env variable to use this function"
        exit 1
    fi
    endpoints=($(kubectl get services -n hyperpilot -l app=influxsrv -o jsonpath='{range .items[?(.spec.type=="LoadBalancer")]}{.status.loadBalancer.ingress[*].hostname}{":"}{.spec.ports[*].targetPort}{" "}{end}'))
    if [[ $? != 0 ]]; then
        #statements
        exit $?
    fi
    for url in "${endpoints[@]}"; do
        echo $url
        h=$(echo $url | awk -F: '{print $1}')
        p=$(echo $url | awk -F: '{print $2}')
        if [[ "$p" == "8086" ]]; then
            #statements
            HOST=$h
        elif [[ "$p" == "8088" ]]; then
            #statements
            BACKUP_HOST=$h:8088
        fi
    done
fi


if [[ "$OPERATION" == "backup" ]]; then
    if [[ -z "$HOST" ]]; then
        HOST="localhost"
    fi

    if [[ -z "$PORT" ]]; then
        #statements
        PORT="8086"
    fi

    if [[ -z $BACKUP_HOST ]]; then
        #statements
        BACKUP_HOST="localhost:8088"
    fi

    if [[ -z "$INFLUX_USERNAME" ]]; then
        INFLUX_USERNAME="root"
    fi

    if [[ -z "$INFLUX_PASSWORD" ]]; then
        INFLUX_PASSWORD="default"
    fi

    if [[ -z "$NAME" ]]; then
        #statements
        echo "please give a snapshot name"
        exit 1
    fi

fi

if [[ "$OPERATION" == "restore" && -z "$NAME" ]]; then
    echo "please give a snapshot name"
    exit 1
fi

echo "OPERATION: $OPERATION"
echo "HOST = $HOST"
echo "PORT = $PORT"
echo "BACKUP_HOST = $BACKUP_HOST"
echo "NAME = $NAME"
echo "INFLUX_USERNAME = $INFLUX_USERNAME"
echo "INFLUX_PASSWORD = $INFLUX_PASSWORD"
echo "KUBECONFIG = $KUBECONFIG"
echo "DATABASE = $DATABASE"

file="$NAME.tar.gz"

case "$OPERATION" in
    backup)
        backup_file_path=$backup_file_path/$NAME
        mkdir -p $backup_file_path
        # backup metastore
        influxd backup -host $BACKUP_HOST $backup_file_path
        ret_code=$?
        if [[ $ret_code != 0 ]]; then
            echo "influxdb backup up failed"
            exit $ret_code
        fi

        # search for databases
        if [[ -z "$DATABASE" ]]; then
            #statements
            dbs=($(influx -host $HOST -port $PORT -username $INFLUX_USERNAME -password $INFLUX_PASSWORD -execute 'show databases' -format json | jq -c '.results[0].series[0].values[] | join([])'))
        else
            dbs=($DATABASE)
        fi

        # backup databases

        echo "Backing up databases $dbs"
        for db in "${dbs[@]}"; do
            normalized_db_name="${db#\"}"
            normalized_db_name="${normalized_db_name%\"}"
            echo "backing up $normalized_db_name"
            echo "influxd backup -host $BACKUP_HOST -database $normalized_db_name $backup_file_path/$normalized_db_name"
            influxd backup -host $BACKUP_HOST -database $normalized_db_name $backup_file_path/$normalized_db_name
            ret_code=$?
            if [[ $ret_code != 0 ]]; then
                echo "error occur while backing up $db"
                exit $ret_code
            fi
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

        ret_code=$?
        if [[ $ret_code != 0 ]]; then
            #statements
            echo "error occur while uploading snapshot to S3"
            exit $ret_code
        fi

        printf "influxDB backup successfully
backup name: $NAME
you can run ./hyperpilot_influx.sh restore command to restore whole database
bye!\n
"
        ;;
    restore)

        # download file from s3 by specified name
        if [[ "$NO_CACHE" == "true" || ! -f $backup_file_path/$file ]]; then
            aws s3 cp s3://$bucket/$NAME.tar.gz $backup_file_path/$file
            ret_code=$?
            if [[ $ret_code != 0 ]]; then
                #statements
                echo "error occur while coping snapshot from S3"
                exit $ret_code
            fi
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

        # kill process
        sudo pkill -f influxd

        ## start restoring
        # restore meta
        sudo influxd restore -metadir $META_DIR $backup_file_path/cache/$NAME
        # restore database
        files=($(ls $backup_file_path/cache/$NAME))
        for db in "${files[@]}"; do
            echo "restoring database $db"
            # restore database
            if ls $backup_file_path/cache/$NAME/$db/$db* 1> /dev/null 2>&1; then
                echo "restoring data"
                sudo influxd restore -database $db -datadir $DATA_DIR $backup_file_path/cache/$NAME/$db
                ret_code=$?
                if [[ $ret_code != 0 ]]; then
                    #statements
                    echo "error restoring database $db"
                    exit $ret_code
                fi
            else
                echo "empty database $db"
            fi
        done

        # update influx deployment
        printf "ok, done.\n please restart your influxd. \nbye!\n"
esac

rm -rf $file
