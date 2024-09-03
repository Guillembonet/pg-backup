#!/bin/bash

DATE_FORMAT=$(date +%Y%m%d%H%M%S)
mkdir -p $BACKUP_DIR
/usr/local/bin/pg_dump -h $PGHOST -p $PGPORT -U $PGUSER -d $PGDATABASE -w > $BACKUP_DIR/$PGDATABASE-$DATE_FORMAT.sql

if [ $? -eq 0 ]; then
    echo "Backup successful"

    find $BACKUP_DIR -type f -name "$PGDATABASE-*.sql" -mtime +$DAYS_OLD -exec rm {} \;

    if [ $? -eq 0 ]; then
        echo "Removed backups older than $DAYS_OLD days"
    else
        echo "Error: Failed to remove old backups"
        exit 1
    fi

else
    echo "Error: pg_dump failed. Old backups not removed."
    exit 1
fi
